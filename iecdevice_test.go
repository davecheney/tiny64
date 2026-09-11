package tiny64

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These tests drive the generic IEC drive the way a C64 really does:
// through the unmodified KERNAL serial routines, one bus transition at a
// time. Nothing here reaches into the drive to short-circuit a transfer,
// so a handshake or a timing constant that is wrong shows up as a hang or
// a corrupted file rather than quietly passing.

// useDrive attaches a generic drive with the given disk and puts the bus
// back the way it was afterwards, since the drive and the disk are
// package-level singletons shared by every test in this package.
func useDrive(t *testing.T, disk []byte) {
	savedBus, savedDisk, savedDrive, savedAttached := iecBus, diskImage, virtualDrive, driveAttached
	t.Cleanup(func() {
		iecBus, diskImage, virtualDrive, driveAttached = savedBus, savedDisk, savedDrive, savedAttached
	})
	InsertDisk(disk)

	// InsertDisk plugs in a 1541 on the grounds that a disk needs
	// something to go in, but these tests want the generic drive
	// answering for device 8. Two devices at one address would fight over
	// the bus, so the 1541 comes back out first.
	AttachDrive(false)
	AttachVirtualDrive(8)
}

// virtualDriveDisk returns a formatted disk carrying one PRG file, built
// by the Go filesystem layer rather than by a drive.
func virtualDriveDisk(t *testing.T, name string, data []byte) []byte {
	t.Helper()
	disk := FormatDisk("TEST DISK", "01")
	saved := diskImage
	diskImage = disk
	defer func() { diskImage = saved }()
	if code := diskWriteFile(name, ftypePRG, data); code != 0 {
		t.Fatalf("writing %q to the test disk: DOS error %d", name, code)
	}
	return disk
}

// helloPRG is a PRG that loads at $0801 and does nothing; only its bytes
// matter, not its meaning.
var helloPRG = []byte{
	0x01, 0x08, // load address $0801
	0x0B, 0x08, 0x0A, 0x00, 0x9E, '2', '0', '6', '1', 0x00, 0x00, 0x00,
}

// TestDriveLoadsFileThroughKERNAL is the end-to-end case: a real KERNAL,
// a real BASIC LOAD, and a drive that has to answer every handshake on
// the wire. If the drive gets the ATN response, the turnaround, EOI or
// the frame handshake wrong, BASIC either hangs or reports an error
// instead of printing READY.
//
// The load is repeated at a range of raster phases, because the KERNAL's
// receive loop is level-triggered and a Bad Line can stall it for most of
// a bit time. A transfer that only works at one phase works by luck: the
// symptom of losing that race is not a failed load but a silently
// corrupt one, so the bytes are compared rather than the screen.
func TestDriveLoadsFileThroughKERNAL(t *testing.T) {
	for _, skew := range []int{0, 13, 34, 55, 89, 144} {
		t.Run(fmt.Sprintf("skew%d", skew), func(t *testing.T) {
			m := newMachine(t)
			useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))

			m.waitForLine(5, "READY.")
			m.run(skew)
			m.typeLine(`LOAD"HELLO",8`)

			m.waitForLine(8, "SEARCHING FOR HELLO")
			m.waitForLine(9, "LOADING")
			m.waitForLine(10, "READY.")

			// BASIC loads at $0801 and leaves the end-of-program pointer
			// at $2D, so the bytes that arrived can be compared against
			// the file.
			end := int(ram[0x2D]) | int(ram[0x2E])<<8
			got, want := ram[0x0801:end], helloPRG[2:]
			if !bytes.Equal(got, want) {
				t.Errorf("loaded %v, want %v", got, want)
			}
		})
	}
}

// TestDriveLoadsCompactPRGThroughKERNAL covers the low-RAM generic-drive
// mode used by TinyGo targets. It must use the same IEC and KERNAL load path
// as the D64-backed drive despite holding only the requested program.
func TestDriveLoadsCompactPRGThroughKERNAL(t *testing.T) {
	m := newMachine(t)
	AttachVirtualPRG(8, "HELLO", helloPRG)

	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"HELLO",8`)
	m.waitForLine(8, "SEARCHING FOR HELLO")
	m.waitForLine(9, "LOADING")
	m.waitForLine(10, "READY.")

	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	if got, want := ram[0x0801:end], helloPRG[2:]; !bytes.Equal(got, want) {
		t.Errorf("loaded %v, want %v", got, want)
	}
}

// TestDriveReportsFileNotFound checks the failure path, which is not an
// error code on the wire at all: the drive simply lets go of both lines
// after the turnaround and lets the KERNAL time out.
func TestDriveReportsFileNotFound(t *testing.T) {
	m := newMachine(t)
	useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"NOPE",8`)

	m.waitForLine(8, "SEARCHING FOR NOPE")
	m.waitForLine(9, "?FILE NOT FOUND  ERROR")
}

// TestDriveLoadsDirectory loads "$", which is not a file on the disk at
// all but a BASIC program the drive synthesises, and LISTs it.
func TestDriveLoadsDirectory(t *testing.T) {
	m := newMachine(t)
	useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"$",8`)
	m.waitForLine(10, "READY.")
	m.typeLine("LIST")
	m.waitForLine(16, "READY.")

	// The header line carries the disk name; the entry line carries the
	// file. LIST renders the reverse-video byte as a control character,
	// so only the quoted parts are checked.
	if got := screenLine(13); !contains(got, `"TEST DISK`) {
		t.Errorf("directory header = %q, want it to name the disk", got)
	}
	if got := screenLine(14); !contains(got, `"HELLO"`) || !contains(got, "PRG") {
		t.Errorf("directory entry = %q, want HELLO listed as PRG", got)
	}
	if got := screenLine(15); !contains(got, "BLOCKS FREE") {
		t.Errorf("directory footer = %q, want the blocks-free line", got)
	}
}

// TestDriveSavesAndReloads writes a file the whole way out through the
// bus and reads it back, which exercises the drive as a listener for a
// data stream (rather than just for filenames) and the BAM/directory
// allocation underneath.
func TestDriveSavesAndReloads(t *testing.T) {
	m := newMachine(t)
	useDrive(t, FormatDisk("BLANK", "01"))

	m.waitForLine(5, "READY.")

	// Type in a one-line program, then save it.
	m.typeLine(`10 PRINT"X"`)
	m.typeLine(`SAVE"PROG",8`)
	m.waitForLine(9, "SAVING PROG")
	m.waitForLine(10, "READY.")

	entry, found := diskFind("PROG", ftypePRG)
	if !found {
		t.Fatalf("after SAVE, PROG is not in the directory: %v", diskDirectory())
	}
	saved := diskReadFile(entry)
	if len(saved) < 3 || saved[0] != 0x01 || saved[1] != 0x08 {
		t.Fatalf("saved file = %v, want it to start with the $0801 load address", saved)
	}
}

// TestDriveReportsStatusOnChannel15 reads the command channel, which is
// the drive talking with no file involved at all. It has to be done from
// a program rather than at the prompt because INPUT# is illegal in direct
// mode.
func TestDriveReportsStatusOnChannel15(t *testing.T) {
	m := newMachine(t)
	useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.waitForLine(5, "READY.")
	m.typeLine("10 OPEN1,8,15:INPUT#1,A,B$,C,D")
	m.typeLine("20 PRINTA:PRINTB$")
	m.typeLine("RUN")

	// Nothing has cleared the status yet, so it is still the power-on
	// message, which INPUT# splits on the commas into 73, the message,
	// and the track and sector.
	m.waitForLine(9, " 73")
	if got := screenLine(10); got != "CBM DOS V2.6 1541" {
		t.Errorf("status message = %q, want the power-on message", got)
	}
}

func contains(haystack, needle string) bool {
	return len(needle) <= len(haystack) && bytes.Contains([]byte(haystack), []byte(needle))
}

// TestDriveAttachedBeforeReset covers the order a front end uses. Every
// other test here builds the machine first and attaches afterwards,
// because newMachine ends in Reset(); a -disk flag does the opposite,
// inserting the disk and attaching the drive before the machine is ever
// started. Nothing in the device should depend on the C64 having been
// reset first, but that is worth a test rather than an assumption.
func TestDriveAttachedBeforeReset(t *testing.T) {
	saveMachine(t)

	cpu, cia1, cia2 = CPU{}, CIA{}, CIA{}
	keyboard = Keyboard{}
	bus.Remove()
	vic = VICII{}
	for i := range ram {
		ram[i] = 0
	}
	colorRAM = [1024]byte{}

	// Drive on the bus before the machine is started, not after.
	InsertDisk(virtualDriveDisk(t, "HELLO", helloPRG))
	AttachDrive(false)
	AttachVirtualDrive(8)

	Reset()
	m := &machine{t: t}

	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"HELLO",8`)
	m.waitForLine(8, "SEARCHING FOR HELLO")
	m.waitForLine(10, "READY.")

	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	if got, want := ram[0x0801:end], helloPRG[2:]; !bytes.Equal(got, want) {
		t.Errorf("loaded %v, want %v", got, want)
	}
}

// TestDriveRecoversFromResetMidTransfer resets the C64 in the middle of a
// load. RESET is a wire on the C64, not on the IEC bus, so nothing tells
// the drive the machine went away: it is left part way through sending a
// byte, holding CLOCK or DATA. What rescues it is ATN, which aborts
// whatever a device is doing - the same mechanism a real 1541 relies on,
// since it has no other way of hearing about a reset either.
//
// The reset is delayed by a varying number of cycles, for two reasons at
// once: it lands at a different point in the byte the device is sending,
// and it lands at a different point in the instruction the CPU is
// executing. Resetting mid-instruction is its own hazard (see
// TestResetMidInstructionReboots), and a single fixed offset would only
// ever exercise whichever combination it happened to hit.
func TestDriveRecoversFromResetMidTransfer(t *testing.T) {
	for _, delay := range []int{0, 1, 2, 3, 5, 7, 11, 13, 17, 19, 23, 29} {
		t.Run(fmt.Sprintf("delay%d", delay), func(t *testing.T) {
			m := newMachine(t)
			useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))

			m.waitForLine(5, "READY.")
			m.typeLine(`LOAD"HELLO",8`)
			m.waitForLine(9, "LOADING")
			m.run(delay)

			// Mid-transfer: the device is driving the bus right now.
			if virtualDrive.state == iecIdle {
				t.Fatalf("test is not exercising anything: device already idle")
			}
			m.reset(5, "READY.")

			m.typeLine(`LOAD"HELLO",8`)
			m.waitForLine(8, "SEARCHING FOR HELLO")
			m.waitForLine(10, "READY.")

			end := int(ram[0x2D]) | int(ram[0x2E])<<8
			if got, want := ram[0x0801:end], helloPRG[2:]; !bytes.Equal(got, want) {
				t.Errorf("loaded %v after reset, want %v", got, want)
			}
		})
	}
}

// TestDrivesAgreeOnDirectoryText compares the directory the generic drive
// synthesises against the one the 1541 produces, on the same image, in
// the same machine, byte for byte on the screen.
//
// The 1541 is the oracle here, and not merely because it came first: the
// text it puts on the screen is formatted by the DOS ROM itself, so it is
// the real drive's output by construction. The generic drive
// reimplements that formatting in Go, so it is the side that can drift.
// This test is the cheapest possible check on the whole DOS layer,
// because anything the ROM does that the Go does not is a screen diff.
//
// The names are deliberately sixteen characters, the width of the name
// field, because that is the length that pins the column after it: the
// padding loop runs zero times and only the closed-file flag is left, so
// an off-by-one in the padding cannot hide behind a short name.
func TestDrivesAgreeOnDirectoryText(t *testing.T) {
	saveMachine(t)
	disk := FormatDisk("COMPARE DISK", "01")
	diskImage = disk
	for _, name := range []string{
		"04.LAST NIGHT 30", // exactly sixteen, the pinning case
		"A",                // one, the longest padding run
		"MIDDLING NAME",
	} {
		if code := diskWriteFile(name, ftypePRG, helloPRG); code != 0 {
			t.Fatalf("writing %q: DOS error %d", name, code)
		}
	}

	// A $A0 is written into the header field below, because that is what
	// CBM DOS pads it with and it is the case the real images here do not
	// isolate: it must reach the screen as a space, since LIST renders
	// $A0 in a BASIC line as the token CLOSE.
	if bam := diskReadSector(dirTrack, 0); bam != nil {
		bam[0xA4] = 0xA0
	}

	real, virtual := listDirectoryOnBothDrives(t, disk)

	// Two blank screens compare equal, so the oracle has to be shown to
	// have said something before its agreement means anything.
	if !containsAny(real, "COMPARE DISK") {
		t.Fatalf("the 1541 listed no directory header, so the comparison proved nothing: %q", real)
	}
	for i := range real {
		if real[i] != virtual[i] {
			t.Errorf("screen row %d differs:\n1541    %q\nvirtual %q", i, real[i], virtual[i])
		}
	}
}

// waitForListing runs until a directory listing has finished being
// printed: the screen carries the drive's "BLOCKS FREE." trailer and
// BASIC is back at a prompt.
//
// Both halves are needed, and neither is a cycle count. Counting prompts
// does not survive a directory long enough to scroll, because the earlier
// ones leave the screen. Waiting for the screen to stop changing is worse
// than useless here: during a 1541 load it is legitimately static for
// long stretches while the drive works, so that returns mid-transfer and
// reports a half-drawn listing. That failure reads exactly like a
// formatting difference between the drives while actually being a race,
// which is the one confusion this comparison must not invite.
func waitForListing(m *machine) {
	m.t.Helper()
	const budget = 80_000_000
	for spent := 0; spent < budget; spent += 500_000 {
		if lastNonBlankRow() != "" && strings.HasPrefix(lastNonBlankRow(), "READY.") && screenHas("BLOCKS FREE.") {
			return
		}
		m.run(500_000)
	}
	m.t.Fatalf("no completed directory listing after %d cycles; last row %q", budget, lastNonBlankRow())
}

func lastNonBlankRow() string {
	for row := 24; row >= 0; row-- {
		if s := strings.TrimRight(screenLine(row), " "); s != "" {
			return s
		}
	}
	return ""
}

func screenHas(want string) bool {
	for row := range 25 {
		if strings.Contains(screenLine(row), want) {
			return true
		}
	}
	return false
}

// listDirectoryOnBothDrives returns the whole screen after listing disk's
// directory, once through the 1541 and once through the generic drive.
func listDirectoryOnBothDrives(t *testing.T, disk []byte) (real, virtual []string) {
	t.Helper()
	read := func(useVirtual bool) []string {
		m := newMachine(t)
		InsertDisk(disk)
		if useVirtual {
			AttachDrive(false)
			AttachVirtualDrive(8)
		}
		m.waitForLine(5, "READY.")
		m.typeLine(`LOAD"$",8`)
		waitForLoad(m)
		m.typeLine("LIST")
		waitForListing(m)

		var out []string
		for row := range 25 {
			out = append(out, screenLine(row))
		}
		return out
	}
	return read(false), read(true)
}

// waitForLoad waits for BASIC to come back after a LOAD. Real directories
// take longer to read than waitForLine's budget allows.
func waitForLoad(m *machine) {
	m.t.Helper()
	const budget = 80_000_000
	for spent := 0; spent < budget; spent += 500_000 {
		if strings.HasPrefix(lastNonBlankRow(), "READY.") {
			return
		}
		m.run(500_000)
	}
	m.t.Fatalf("no prompt after LOAD in %d cycles; last row %q", budget, lastNonBlankRow())
}

// TestDrivesAgreeOnRealDisks runs the same comparison as
// TestDrivesAgreeOnDirectoryText against images this repository did not
// produce.
//
// This is the stronger form of that test and it is worth having both. A
// synthesised disk is written by the same code that reads it, so the two
// can share an assumption and agree while both being wrong; more
// importantly a generator only ever emits the shapes it knows how to
// emit. Every difference these images exposed was of that kind: a header
// whose five bytes are one string rather than an ID and a DOS version,
// DEL entries occupying live slots, and locked files. None of those can
// arise from FormatDisk and diskWriteFile however many files are written.
//
// The synthesised test is not redundant, because it covers the case real
// disks here do not reach as cheaply: a $A0 in the header, which must
// become a space before LIST renders it as the token CLOSE.
func TestDrivesAgreeOnRealDisks(t *testing.T) {
	for _, name := range []string{"enforcer", "lastnight", "validated"} {
		t.Run(name, func(t *testing.T) {
			disk, err := os.ReadFile(filepath.Join("d64", name+".d64"))
			if err != nil {
				t.Skipf("no %s.d64 to compare against: %v", name, err)
			}
			saveMachine(t)
			real, virtual := listDirectoryOnBothDrives(t, disk)

			// Two blank screens compare equal, so the oracle has to be
			// shown to have said something before agreement means
			// anything.
			if !containsAny(real, "BLOCKS FREE.") {
				t.Fatalf("the 1541 listed no directory, so the comparison proved nothing: %q", real)
			}
			for i := range real {
				if real[i] != virtual[i] {
					t.Errorf("screen row %d differs:\n1541    %q\nvirtual %q", i, real[i], virtual[i])
				}
			}
		})
	}
}

func containsAny(rows []string, want string) bool {
	for _, r := range rows {
		if strings.Contains(r, want) {
			return true
		}
	}
	return false
}

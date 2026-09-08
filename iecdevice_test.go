package tiny64

import (
	"bytes"
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
func TestDriveLoadsFileThroughKERNAL(t *testing.T) {
	m := newMachine(t)
	useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"HELLO",8`)

	m.waitForLine(8, "SEARCHING FOR HELLO")
	m.waitForLine(9, "LOADING")
	m.waitForLine(10, "READY.")

	// BASIC loads at $0801 and leaves the end-of-program pointer at $2D,
	// so the bytes that arrived can be compared against the file.
	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	got := ram[0x0801:end]
	want := helloPRG[2:]
	if !bytes.Equal(got, want) {
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

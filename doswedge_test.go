package tiny64

import (
	"bytes"
	"strings"
	"testing"

	"github.com/davecheney/tiny64/rom"
)

func newWedgeMachine(t *testing.T, disk []byte) *machine {
	t.Helper()
	m := newMachine(t)
	useDrive(t, disk)
	EnableDOSWedge()
	m.reset(5, "READY.")
	m.waitForScreen("DOS WEDGE ACTIVE")
	return m
}

func (m *machine) waitForScreen(want string) {
	m.t.Helper()
	const budget = 80_000_000
	for spent := 0; spent < budget; spent += 100_000 {
		if screenHas(want) {
			return
		}
		m.run(100_000)
	}
	var screen strings.Builder
	for row := range 25 {
		if line := screenLine(row); line != "" {
			screen.WriteString("\n")
			screen.WriteString(line)
		}
	}
	m.t.Fatalf("after %d cycles, screen does not contain %q; PC=$%04X screen:%s",
		budget, want, cpu.PC, screen.String())
}

func TestDOSWedgeDisabledByDefault(t *testing.T) {
	m := newMachine(t)
	useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.waitForLine(5, "READY.")
	m.typeLine("/HELLO")
	m.waitForScreen("?SYNTAX  ERROR")

	if bytes.Equal(ram[0x0801:0x0801+len(helloPRG)-2], helloPRG[2:]) {
		t.Fatal("/HELLO loaded a program while the DOS wedge was disabled")
	}
}

func TestDOSWedgeLoadsPRGThroughBASICAndKERNAL(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.typeLine("/HELLO")
	m.waitForScreen("SEARCHING FOR HELLO")
	m.waitForScreen("LOADING")
	waitForLoad(m)

	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	if got, want := ram[0x0801:end], helloPRG[2:]; !bytes.Equal(got, want) {
		t.Errorf("loaded %v, want %v", got, want)
	}
}

func TestDOSWedgeDirectoryPreservesBASICProgram(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.typeLine(`10 PRINT"STILL HERE"`)
	m.typeLine("@$")
	waitForListing(m)

	if !screenHas(`"TEST DISK`) || !screenHas(`"HELLO"`) || !screenHas("BLOCKS FREE.") {
		t.Fatalf("directory listing is incomplete: last row %q", lastNonBlankRow())
	}

	m.typeLine("LIST")
	m.waitForScreen(`10 PRINT"STILL HERE"`)
}

func TestDOSWedgeStatusAndCommandsUseCommandChannel(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.typeLine("@")
	m.waitForScreen("73,CBM DOS V2.6 1541,00,00")
	m.typeLine(">")
	m.waitForScreen("00,OK,00,00")

	m.typeLine("@S:HELLO")
	m.waitForScreen("01,FILES SCRATCHED,01,00")
	if _, found := diskFind("HELLO", ftypePRG); found {
		t.Fatal("@S:HELLO left HELLO on the disk")
	}

	m.typeLine("@N:NEW DISK,42")
	m.waitForScreen("00,OK,00,00")
	if got := strings.TrimRight(diskName(), string([]byte{0xA0, ' '})); got != "NEW DISK" {
		t.Errorf("formatted disk name = %q, want %q", got, "NEW DISK")
	}
}

func TestDOSWedgeSelectsDevice(t *testing.T) {
	m := newMachine(t)
	AttachVirtualPRG(9, "HELLO", helloPRG)
	EnableDOSWedge()
	m.reset(5, "READY.")
	m.waitForScreen("DOS WEDGE ACTIVE")

	m.typeLine("@#9")
	m.typeLine("/HELLO")
	m.waitForScreen("SEARCHING FOR HELLO")
	waitForLoad(m)

	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	if got, want := ram[0x0801:end], helloPRG[2:]; !bytes.Equal(got, want) {
		t.Errorf("device 9 load = %v, want %v", got, want)
	}
}

func TestDOSWedgeLoadRunAndMachineLoad(t *testing.T) {
	runPRG := []byte{
		0x01, 0x08,
		0x0B, 0x08,
		0x0A, 0x00,
		0x99, ' ', '"', 'X', '"',
		0x00,
		0x00, 0x00,
	}
	m := newWedgeMachine(t, virtualDriveDisk(t, "RUNME", runPRG))

	m.typeLine("↑RUNME")
	m.waitForScreen("SEARCHING FOR RUNME")
	m.waitForScreen("LOADING")
	m.waitForScreen("X")
}

func TestDOSWedgeMachineCodeLoadUsesFileLoadAddress(t *testing.T) {
	mcPRG := []byte{0x00, 0x20, 0xDE, 0xAD, 0xBE, 0xEF}
	m := newWedgeMachine(t, virtualDriveDisk(t, "CODE", mcPRG))

	m.typeLine("%CODE")
	m.waitForScreen("SEARCHING FOR CODE")
	m.waitForScreen("LOADING")
	waitForLoad(m)

	if got, want := ram[0x2000:0x2004], mcPRG[2:]; !bytes.Equal(got, want) {
		t.Errorf("machine-code load bytes = %v, want %v", got, want)
	}
}

func TestDOSWedgeSavesBASICProgram(t *testing.T) {
	m := newWedgeMachine(t, FormatDisk("BLANK", "01"))

	m.typeLine(`10 PRINT"X"`)
	m.typeLine("←PROG")
	m.waitForScreen("SAVING PROG")
	m.waitForScreen("READY.")

	entry, found := diskFind("PROG", ftypePRG)
	if !found {
		t.Fatalf("left-arrow save did not create PROG: %v", diskDirectory())
	}
	if saved := diskReadFile(entry); len(saved) < 3 || saved[0] != 0x01 || saved[1] != 0x08 {
		t.Fatalf("saved file = %v, want it to start with the $0801 load address", saved)
	}
}

func TestDOSWedgeDeactivateAndReactivate(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.typeLine("@Q")
	m.typeLine("/HELLO")
	m.waitForScreen("?SYNTAX  ERROR")
	if bytes.Equal(ram[0x0801:0x0801+len(helloPRG)-2], helloPRG[2:]) {
		t.Fatal("/HELLO loaded a program after @Q deactivated the wedge")
	}

	m.typeLine("SYS 32777")
	m.waitForScreen("DOS WEDGE ACTIVE")
	m.typeLine("/HELLO")
	m.waitForScreen("SEARCHING FOR HELLO")
	waitForLoad(m)
}

func TestDOSWedgeDoesNotInterceptStoredBASICLines(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.typeLine("10 /HELLO")
	m.typeLine("RUN")
	m.waitForScreen("?SYNTAX  ERROR")
	if bytes.Equal(ram[0x0801:0x0801+len(helloPRG)-2], helloPRG[2:]) {
		t.Fatal("stored BASIC line loaded a program through the direct-mode wedge")
	}
}

// TestDOSWedgeCartridgeImage checks the parts of the ROM image the KERNAL
// and the user reach by fixed address rather than by following a vector:
// the autostart signature, and the JMP that SYS 32777 lands on.
func TestDOSWedgeCartridgeImage(t *testing.T) {
	img := buildDOSWedge()

	if len(img) != dosWedgeSize {
		t.Fatalf("image is %d bytes, want exactly %d - an 8K EPROM", len(img), dosWedgeSize)
	}
	if got, want := img[4:9], []byte{0xC3, 0xC2, 0xCD, 0x38, 0x30}; !bytes.Equal(got, want) {
		t.Errorf("signature at $8004 = % X, want % X (CBM80)", got, want)
	}
	if got := img[dosWedgeReactivate-dosWedgeOrigin]; got != 0x4C {
		t.Errorf("byte at SYS %d = %#02x, want %#02x (JMP)", dosWedgeReactivate, got, 0x4C)
	}
	if got := img[len(img)-1]; got != 0xFF {
		t.Errorf("last byte = %#02x, want %#02x - the image must be padded to fill the EPROM", got, 0xFF)
	}

	// The cold start and NMI vectors, and the target of that JMP, all have
	// to point at code inside the cartridge rather than at the zeros a
	// half-built image would leave behind.
	for _, vec := range []struct {
		at   int
		name string
	}{
		{0, "cold start vector"},
		{2, "NMI vector"},
		{dosWedgeReactivate - dosWedgeOrigin + 1, "SYS reactivate target"},
	} {
		addr := uint16(img[vec.at]) | uint16(img[vec.at+1])<<8
		if addr < dosWedgeOrigin+12 || addr > dosWedgeOrigin+dosWedgeSize-1 {
			t.Errorf("%s = $%04X, want an address inside the cartridge", vec.name, addr)
		}
	}
}

// TestDOSWedgeEntryPointsMatchROM pins the ROM addresses the cartridge
// jumps into that have no jump table entry, and so are specific to this
// revision of the KERNAL. Swapping in another ROM should fail here, where
// the reason is written down, rather than as a machine that boots to a
// blank screen somewhere else in the suite.
func TestDOSWedgeEntryPointsMatchROM(t *testing.T) {
	kernal := func(addr uint16, n int) []byte {
		return rom.Kernal[addr-0xE000 : addr-0xE000+uint16(n)]
	}

	for _, want := range []struct {
		addr  uint16
		bytes []byte
		what  string
	}{
		{0xFCEC, []byte{0x6C, 0x00, 0x80}, "reset hands a CBM80 cartridge control through $8000"},
		{0xFE5B, []byte{0x6C, 0x02, 0x80}, "the NMI handler jumps through $8002 as well"},
		{kernalNMIResume, []byte{0x20, 0xBC, 0xF6}, "kernalNMIResume is the instruction after that jump"},
		{0xE394, []byte{0x20, 0x53, 0xE4, 0x20, 0xBF, 0xE3, 0x20, 0x22, 0xE4}, "BASIC's cold start is the three calls the cartridge makes"},
		{basicColdTail, []byte{0xA2, 0x80, 0x6C, 0x00, 0x03}, "the cold start tail reaches READY. through $0300"},
		{0xE449, []byte{0x83, 0xA4}, "$E453 copies $A483 into $0302, which the cartridge then replaces"},
	} {
		if got := kernal(want.addr, len(want.bytes)); !bytes.Equal(got, want.bytes) {
			t.Errorf("KERNAL $%04X = % X, want % X: %s", want.addr, got, want.bytes, want.what)
		}
	}
}

// TestDOSWedgeBootsFromCartridgeSignature checks that the machine really
// did boot the way a machine with a cartridge in it boots: nothing in Go
// touches $0302, so finding it pointing into the cartridge's own ROM means
// the KERNAL found the signature and handed over.
func TestDOSWedgeBootsFromCartridgeSignature(t *testing.T) {
	newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	main := uint16(ram[0x0302]) | uint16(ram[0x0303])<<8
	if main < dosWedgeOrigin || main > dosWedgeOrigin+dosWedgeSize-1 {
		t.Errorf("$0302 = $%04X, want an address in cartridge ROM", main)
	}
	if got, want := bus.Load(0x8004), byte(0xC3); got != want {
		t.Errorf("load($8004) = %#02x, want %#02x - the cartridge must be visible to the CPU", got, want)
	}
}

// TestDOSWedgeLeavesRAMAlone is the regression guard for what the
// cartridge replaced: a Go-side hook that wrote three kilobytes into the
// middle of the machine's RAM behind the CPU's back. The wedge's only RAM
// is its workspace in the cassette buffer.
func TestDOSWedgeLeavesRAMAlone(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))
	m.typeLine("@#9")

	for addr := 0xC000; addr < 0xD000; addr++ {
		if ram[addr] != 0 {
			t.Fatalf("ram[$%04X] = %#02x, want 0: the wedge must not live in RAM any more", addr, ram[addr])
		}
	}
	if got := ram[wedgeCurrentDevice]; got != 9 {
		t.Errorf("ram[$%04X] = %d, want 9 - the workspace belongs in the cassette buffer", wedgeCurrentDevice, got)
	}
	for addr := wedgeWorkEnd; addr < 0x03FC; addr++ {
		if ram[addr] != 0 {
			t.Errorf("ram[$%04X] = %#02x, want 0: the workspace must stay inside its own bounds", addr, ram[addr])
		}
	}
}

// TestDOSWedgeReportsCartridgeMemoryTop checks the free-memory line, which
// nothing in the emulator arranges: RAMTAS walks up from $0400 writing and
// reading back, finds this cartridge where RAM should be, and sets the top
// of BASIC memory below it. 38911 - 8192 = 30719.
func TestDOSWedgeReportsCartridgeMemoryTop(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))
	if !screenHas("30719 BASIC BYTES FREE") {
		t.Errorf("boot screen does not report 30719 bytes free: row 3 is %q", screenLine(3))
	}

	// And with an empty expansion port, all of it is still there.
	DisableDOSWedge()
	m.reset(5, "READY.")
	if !screenHas("38911 BASIC BYTES FREE") {
		t.Errorf("after unplugging the cartridge, row 3 is %q, want 38911 bytes free", screenLine(3))
	}
}

// TestDOSWedgeSurvivesRestore checks the cartridge's NMI vector. The
// KERNAL's NMI handler runs the same signature check its reset does and
// jumps through $8002, so a cartridge that leaves that vector pointing
// anywhere careless crashes on the first RESTORE.
func TestDOSWedgeSurvivesRestore(t *testing.T) {
	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	keyboard.Restore()
	m.run(cyclesPerKeyPhase)
	keyboard.ReleaseAll()
	m.run(cyclesPerKeyPhase)

	// The wedge is still there afterwards - warm start does not rewrite
	// $0302, and the cartridge is still mapped.
	m.typeLine("/HELLO")
	m.waitForScreen("SEARCHING FOR HELLO")
	waitForLoad(m)

	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	if got, want := ram[0x0801:end], helloPRG[2:]; !bytes.Equal(got, want) {
		t.Errorf("after RESTORE, load = %v, want %v", got, want)
	}
}

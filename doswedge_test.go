package tiny64

import (
	"bytes"
	"strings"
	"testing"
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

	m.typeLine("SYS 52224")
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

func TestDOSWedgeActivationIsResetDriven(t *testing.T) {
	m := newMachine(t)
	useDrive(t, virtualDriveDisk(t, "HELLO", helloPRG))
	m.waitForLine(5, "READY.")

	EnableDOSWedge()
	m.typeLine("/HELLO")
	m.waitForScreen("?SYNTAX  ERROR")

	m.reset(5, "READY.")
	m.waitForScreen("DOS WEDGE ACTIVE")
	m.typeLine("/HELLO")
	m.waitForScreen("SEARCHING FOR HELLO")
	waitForLoad(m)
}

package tiny64

import (
	"bytes"
	"fmt"
	"slices"
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

func (m *machine) runUntil(what string, budget int, reached func() bool) {
	m.t.Helper()
	for range budget {
		if reached() {
			return
		}
		vic.StepCycle()
	}
	m.t.Fatalf("did not reach %s; PC=$%04X", what, cpu.PC)
}

func assertWedgeRAM(t *testing.T) {
	t.Helper()
	if cartridge.Exrom {
		t.Fatal("cartridge still asserts EXROM")
	}
	for addr := uint16(0x8000); addr < 0xA000; addr++ {
		if got := plaLoad(addr); got != ram[addr] {
			t.Fatalf("load($%04X)=$%02X, RAM=$%02X", addr, got, ram[addr])
		}
	}
}

func TestDOSWedgeDisabledByDefault(t *testing.T) {
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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

func TestDOSWedgeQuitUntilReset(t *testing.T) {
	skipShort(t)

	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	m.press(false, KeyAt)
	m.press(false, KeyQ)
	keyboard.Press(KeyReturn)
	m.runUntil("CPU cartridge-kill write", 100_000, func() bool {
		return !bus.RW && bus.Address == dosWedgeLatch && bus.Data == dosWedgeKill
	})
	if cpu.PC < wedgeRAMKill || cpu.PC > wedgeRAMKill+3 {
		t.Fatalf("cartridge killed from PC=$%04X, not the RAM trampoline", cpu.PC)
	}
	if hook := uint16(ram[0x0302]) | uint16(ram[0x0303])<<8; hook != basicWarmStart {
		t.Fatalf("kill left prompt hook at $%04X", hook)
	}
	keyboard.ReleaseAll()
	m.run(cyclesPerKeyPhase)
	assertWedgeRAM(t)
	m.typeLine("/HELLO")
	m.waitForScreen("?SYNTAX  ERROR")
	if bytes.Equal(ram[0x0801:0x0801+len(helloPRG)-2], helloPRG[2:]) {
		t.Fatal("/HELLO loaded a program after @Q deactivated the wedge")
	}

	if !cartridge.killed || cartridge.Exrom {
		t.Fatal("@Q did not lock out the cartridge")
	}
	for _, stop := range []bool{false, true} {
		if stop {
			keyboard.Press(KeyRunStop)
		}
		keyboard.Restore()
		m.run(cyclesPerKeyPhase)
		keyboard.ReleaseAll()
		m.run(cyclesPerKeyPhase)
		if !cartridge.killed {
			t.Fatal("RESTORE reactivated the cartridge")
		}
	}
	m.reset(5, "READY.")
	m.waitForScreen("DOS WEDGE ACTIVE")
	m.typeLine("/HELLO")
	m.waitForScreen("SEARCHING FOR HELLO")
	waitForLoad(m)
}

func TestDOSWedgeFullMemoryProgramAndSave(t *testing.T) {
	skipShort(t)

	// A BASIC program whose last line lives above $8000, followed by string
	// allocation near $A000. The lines and string must not read cartridge ROM.
	prg := []byte{0x01, 0x08}
	for line := 1; line <= 200; line++ {
		text := append([]byte{0x8F}, bytes.Repeat([]byte{'X'}, 150)...) // REM
		if line == 200 {
			text = []byte{0x99, '"', 'P', 'A', 'S', 'S', '"'} // PRINT"PASS"
		}
		next := 0x0801 + len(prg) - 2 + 4 + len(text) + 1
		prg = append(prg, byte(next), byte(next>>8), byte(line), byte(line>>8))
		prg = append(prg, text...)
		prg = append(prg, 0)
	}
	prg = append(prg, 0, 0)
	m := newWedgeMachine(t, virtualDriveDisk(t, "BIG", prg))
	m.typeLine("/BIG")
	m.waitForScreen("LOADING")
	waitForLoad(m)
	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	if end <= 0x8000 || !bytes.Equal(ram[0x0801:end], prg[2:]) {
		t.Fatalf("full-memory BASIC load failed; end=$%04X", end)
	}
	assertWedgeRAM(t)
	m.typeLine("RUN")
	m.waitForScreen("PASS")
	m.typeLine(`A$="AB"+"CD":PRINT A$`)
	m.waitForScreen("ABCD")
	if stringsBottom := uint16(ram[0x33]) | uint16(ram[0x34])<<8; stringsBottom < 0x8000 {
		t.Fatalf("string allocation did not exercise high RAM: $%04X", stringsBottom)
	}
	m.typeLine("←COPY")
	m.waitForScreen("SAVING COPY")
	waitForLoad(m)
	entry, ok := diskFind("COPY", ftypePRG)
	if !ok || !bytes.Equal(diskReadFile(entry), prg) {
		t.Fatal("SAVE did not preserve BASIC bytes above $8000")
	}
	assertWedgeRAM(t)
}

func TestDOSWedgeFailedLoadsLeaveRAMVisible(t *testing.T) {
	skipShort(t)

	for _, command := range []string{"/MISSING", "%MISSING", "↑MISSING"} {
		t.Run(command, func(t *testing.T) {
			m := newWedgeMachine(t, FormatDisk("EMPTY", "00"))
			m.typeLine("10 POKE49152,42")
			m.typeLine(command)
			m.waitForScreen("?FILE NOT FOUND")
			m.run(500_000)
			assertWedgeRAM(t)
			want := byte(0)
			if command == "↑MISSING" {
				want = 42 // historical queued RUN also runs after a failed LOAD
			}
			if ram[0xC000] != want {
				t.Fatalf("stale program marker=%d, want %d", ram[0xC000], want)
			}
		})
	}
}

func TestDOSWedgeRESTOREDuringCommand(t *testing.T) {
	skipShort(t)

	for _, phase := range []string{"entry", "ROM", "call", "service", "return", "exit"} {
		for _, stop := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stop=%v", phase, stop), func(t *testing.T) {
				m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))
				m.press(false, KeyAt)
				keyboard.Press(KeyReturn)
				m.runUntil(phase, 2_000_000, func() bool {
					if cpu.TState != 0 {
						return false
					}
					switch phase {
					case "entry":
						return cpu.PC >= dosWedgeIO && cartridge.Exrom
					case "ROM":
						return cpu.PC >= 0x8000 && cpu.PC < 0x9F00
					case "call":
						return cpu.PC == wedgeRAMGate
					case "service":
						return cpu.PC == 0xFFBD
					case "return":
						return cpu.PC == wedgeRAMCall+3
					case "exit":
						return cpu.PC >= dosWedgeIO && bus.RW == false && bus.Address == dosWedgeLatch && bus.Data == 0
					}
					return false
				})
				keyboard.ReleaseAll()
				if stop {
					keyboard.Press(KeyRunStop)
				}
				keyboard.Restore()
				m.run(cyclesPerKeyPhase)
				keyboard.ReleaseAll()
				m.run(500_000)
				if stop {
					m.waitForLine(1, "READY.")
				} else {
					m.waitForScreen("73,CBM DOS V2.6 1541,00,00")
				}
				assertWedgeRAM(t)
				m.typeLine("/HELLO")
				m.waitForScreen("LOADING")
				waitForLoad(m)
				if !bytes.Equal(ram[0x0801:0x0801+len(helloPRG)-2], helloPRG[2:]) {
					t.Fatal("wedge did not recover after NMI")
				}
			})
		}
	}
}

func TestDOSWedgeNMIGateBoundaries(t *testing.T) {
	skipShort(t)

	m := newWedgeMachine(t, FormatDisk("EMPTY", "00"))
	prompt := uint16(ram[wedgeRAMEntry+1]) | uint16(ram[wedgeRAMEntry+2])<<8
	m.press(false, KeyAt)
	keyboard.Press(KeyReturn)
	var boundaries []uint16
	m.runUntil("command returning to prompt", 2_000_000, func() bool {
		if cpu.TState == 0 && cpu.PC >= wedgeRAMGate && cpu.PC < wedgeWorkEnd && !slices.Contains(boundaries, cpu.PC) {
			boundaries = append(boundaries, cpu.PC)
		}
		return len(boundaries) != 0 && cpu.TState == 0 && cpu.PC == prompt
	})
	keyboard.ReleaseAll()
	slices.Sort(boundaries)
	if len(boundaries) < 20 {
		t.Fatalf("only observed %d gate instruction boundaries", len(boundaries))
	}
	for _, boundary := range boundaries {
		for _, stop := range []bool{false, true} {
			t.Run(fmt.Sprintf("%04X/stop=%v", boundary, stop), func(t *testing.T) {
				m := newWedgeMachine(t, FormatDisk("EMPTY", "00"))
				m.press(false, KeyAt)
				keyboard.Press(KeyReturn)
				m.runUntil("gate instruction", 2_000_000, func() bool {
					return cpu.TState == 0 && cpu.PC == boundary
				})
				keyboard.ReleaseAll()
				if stop {
					keyboard.Press(KeyRunStop)
				}
				keyboard.Restore()
				m.run(cyclesPerKeyPhase)
				keyboard.ReleaseAll()
				m.run(500_000)
				if stop {
					m.waitForLine(1, "READY.")
				} else {
					m.waitForScreen("73,CBM DOS V2.6 1541,00,00")
				}
				assertWedgeRAM(t)
				m.typeLine("@#9")
				if ram[wedgeCurrentDevice] != 9 {
					t.Fatal("prompt did not recover after NMI")
				}
			})
		}
	}
}

func TestDOSWedgeServiceGatePreservesRegisters(t *testing.T) {
	skipShort(t)

	service := newWedgeAssembler(0x9000)
	service.abs(0x8D, 0xC010)
	service.abs(0x8E, 0xC011)
	service.abs(0x8C, 0xC012)
	service.emit(0x08, 0x68) // PHP; PLA
	service.abs(0x8D, 0xC013)
	service.emit(0xA2, 0x42, 0xA0, 0x24, 0xA9, 0x80, 0x38, 0x60)

	a := newWedgeAssembler(0x080D)
	a.emit(0xA2, byte(len(service.code)-1))
	a.label("copy")
	a.ref(0xBD, "service")
	a.abs(0x9D, 0x9000)
	a.emit(0xCA)
	a.branch(0x10, "copy")
	a.emit(0xA2, 0x41, 0xA0, 0x43, 0xA9, 0x37, 0xB8, 0x38) // CLV; SEC
	a.service(0x9000)
	a.emit(0x08)
	a.abs(0x8D, 0xC020)
	a.abs(0x8E, 0xC021)
	a.abs(0x8C, 0xC022)
	a.emit(0x68)
	a.abs(0x8D, 0xC023)
	a.emit(0xA9, 0)
	a.abs(0x8D, dosWedgeLatch)
	a.emit(0x58, 0x60) // CLI; RTS
	a.label("service")
	a.emit(service.finish()...)
	prg := append(append([]byte(nil), helloPRG...), a.finish()...)
	m := newWedgeMachine(t, virtualDriveDisk(t, "GATE", prg))
	m.typeLine("↑GATE")
	m.runUntil("service result", 5_000_000, func() bool { return ram[0xC023] != 0 })
	m.run(100_000)
	if got, want := ram[0xC010:0xC014], []byte{0x37, 0x41, 0x43, 0x31}; !bytes.Equal(got, want) {
		t.Fatalf("service arguments A/X/Y/P = % X, want % X", got, want)
	}
	if got, want := ram[0xC020:0xC024], []byte{0x80, 0x42, 0x24, 0xB5}; !bytes.Equal(got, want) {
		t.Fatalf("service result A/X/Y/P = % X, want % X (IRQ masked on return to firmware)", got, want)
	}
	assertWedgeRAM(t)
}

func TestDOSWedgeIRQHandlerInHighRAM(t *testing.T) {
	skipShort(t)

	handler := []byte{
		0xEE, 0x00, 0xC0, // INC $C000
		0xD0, 0x03,
		0xEE, 0x01, 0xC0,
		0x4C, 0x31, 0xEA, // JMP KERNAL IRQ
	}
	a := newWedgeAssembler(0x080D)
	a.emit(0x78, 0xA2, byte(len(handler)-1))
	a.label("copy")
	a.ref(0xBD, "handler")
	a.abs(0x9D, 0x9000)
	a.emit(0xCA)
	a.branch(0x10, "copy")
	a.emit(0xA9, 0)
	a.abs(0x8D, 0x0314)
	a.emit(0xA9, 0x90)
	a.abs(0x8D, 0x0315)
	a.emit(0x58, 0x60)
	a.label("handler")
	a.emit(handler...)
	prg := append(append([]byte(nil), helloPRG...), a.finish()...)
	m := newWedgeMachine(t, virtualDriveDisk(t, "IRQ", prg))
	m.typeLine("↑IRQ")
	m.runUntil("RAM IRQ", 5_000_000, func() bool { return ram[0xC000] != 0 })
	for _, command := range []string{"@", "@$", "@", "←COPY", "@", "/IRQ"} {
		before := uint16(ram[0xC000]) | uint16(ram[0xC001])<<8
		m.typeLine(command)
		waitForLoad(m)
		after := uint16(ram[0xC000]) | uint16(ram[0xC001])<<8
		if after <= before {
			t.Fatalf("IRQ stopped during %s: before=%d after=%d", command, before, after)
		}
		assertWedgeRAM(t)
	}
}

func TestDOSWedgeDoesNotInterceptStoredBASICLines(t *testing.T) {
	skipShort(t)

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
// the autostart signature and its bootstrap/NMI entry points in I/O ROM.
func TestDOSWedgeCartridgeImage(t *testing.T) {
	skipShort(t)

	img := buildDOSWedge()

	if len(img) != dosWedgeSize {
		t.Fatalf("image is %d bytes, want exactly %d - an 8K EPROM", len(img), dosWedgeSize)
	}
	if got, want := img[4:9], []byte{0xC3, 0xC2, 0xCD, 0x38, 0x30}; !bytes.Equal(got, want) {
		t.Errorf("signature at $8004 = % X, want % X (CBM80)", got, want)
	}
	if got := img[len(img)-1]; got != 0xFF {
		t.Errorf("last byte = %#02x, want %#02x - the image must be padded to fill the EPROM", got, 0xFF)
	}

	// The vectors execute in I/O ROM so releasing EXROM cannot remove them.
	for _, vec := range []struct {
		at   int
		name string
	}{
		{0, "cold start vector"},
		{2, "NMI vector"},
	} {
		addr := uint16(img[vec.at]) | uint16(img[vec.at+1])<<8
		if addr < dosWedgeIO || addr >= dosWedgeLatch {
			t.Errorf("%s = $%04X, want an address inside cartridge I/O", vec.name, addr)
		}
	}
}

// TestDOSWedgeEntryPointsMatchROM pins the ROM addresses the cartridge
// jumps into that have no jump table entry, and so are specific to this
// revision of the KERNAL. Swapping in another ROM should fail here, where
// the reason is written down, rather than as a machine that boots to a
// blank screen somewhere else in the suite.
func TestDOSWedgeEntryPointsMatchROM(t *testing.T) {
	skipShort(t)

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
		{0xFE5E, []byte{0x20, 0xBC, 0xF6, 0x20, 0xE1, 0xFF, 0xD0, 0x0C}, "NMI scans STOP before choosing return or warm start"},
		{kernalNMIWarm, []byte{0x20, 0x15, 0xFD}, "NMI warm start restores vectors"},
		{kernalNMIReturn, []byte{0x98, 0x2D, 0xA1, 0x02}, "NMI return processes CIA2 status"},
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
// touches $0302, so finding it pointing into the installed dispatcher means
// the KERNAL found the signature and handed over.
func TestDOSWedgeBootsFromCartridgeSignature(t *testing.T) {
	skipShort(t)

	newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	main := uint16(ram[0x0302]) | uint16(ram[0x0303])<<8
	if main != wedgeRAMEntry {
		t.Errorf("$0302 = $%04X, want cassette dispatcher $%04X", main, wedgeRAMEntry)
	}
	if got, want := bus.Load(0x8004), ram[0x8004]; got != want || cartridge.Exrom {
		t.Errorf("load($8004) = %#02x, want RAM %#02x with ROML hidden", got, want)
	}
}

// TestDOSWedgeLeavesRAMAlone is the regression guard for what the
// cartridge replaced: a Go-side hook that wrote three kilobytes into the
// middle of the machine's RAM behind the CPU's back. The wedge's only RAM
// is its workspace in the cassette buffer.
func TestDOSWedgeLeavesRAMAlone(t *testing.T) {
	skipShort(t)

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
// nothing in the emulator arranges: RAMTAS must see RAM, not cartridge ROM.
func TestDOSWedgeReportsCartridgeMemoryTop(t *testing.T) {
	skipShort(t)

	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))
	if !screenHas("38911 BASIC BYTES FREE") {
		t.Errorf("boot screen does not report 38911 bytes free: row 3 is %q", screenLine(3))
	}
	for _, addr := range []uint16{0x0283, 0x0037} {
		if top := uint16(ram[addr]) | uint16(ram[addr+1])<<8; top != 0xA000 {
			t.Errorf("memory top at $%04X = $%04X, want $A000", addr, top)
		}
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
	skipShort(t)

	m := newWedgeMachine(t, virtualDriveDisk(t, "HELLO", helloPRG))

	keyboard.Restore()
	m.run(cyclesPerKeyPhase)
	keyboard.ReleaseAll()
	m.run(cyclesPerKeyPhase)

	// The wedge is still there afterwards - warm start does not rewrite
	// $0302, and the cartridge can still service commands.
	m.typeLine("/HELLO")
	m.waitForScreen("SEARCHING FOR HELLO")
	waitForLoad(m)

	end := int(ram[0x2D]) | int(ram[0x2E])<<8
	if got, want := ram[0x0801:end], helloPRG[2:]; !bytes.Equal(got, want) {
		t.Errorf("after RESTORE, load = %v, want %v", got, want)
	}
}

package tiny64

import "testing"

func TestVICBadLineBusWarning(t *testing.T) {
	newMachine(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	cpu.PC = 0x0200
	for i := 0; i < 128; i++ {
		ram[0x0200+i] = 0xEA
	}
	vic.rasterLine, vic.dot = 0x33, 0
	vic.control1, vic.allowBadLine = 0x13, true
	vic.syncLineVisibility()

	var warnings, stolen int
	for slot := 0; slot <= 45; slot++ {
		pc, ts := cpu.PC, cpu.TState
		vic.StepCycle()
		// The start-of-StepCycle slot is article cycle minus 11.
		wantBA := slot < 1 || slot > 43  // Article cycles 12-54.
		wantAEC := slot < 4 || slot > 43 // Article cycles 15-54.
		if vic.BA != wantBA || vic.AEC != wantAEC {
			t.Fatalf("article cycle %d: BA/AEC=%v/%v, want %v/%v",
				slot+11, vic.BA, vic.AEC, wantBA, wantAEC)
		}
		if !wantBA && wantAEC {
			warnings++
		}
		if !wantAEC {
			stolen++
		}
		if !wantBA && (cpu.PC != pc || cpu.TState != ts) {
			t.Fatalf("article cycle %d: read advanced during BA hold", slot+11)
		}
		if slot == 44 && cpu.PC == pc && cpu.TState == ts {
			t.Fatal("CPU did not resume on bus release")
		}
	}
	if warnings != 3 || stolen != 40 {
		t.Fatalf("warning/stolen cycles=%d/%d, want 3/40", warnings, stolen)
	}
}

func TestVICLateBadLineWarning(t *testing.T) {
	newMachine(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	cpu.PC = 0x0200
	ram[0x0200] = 0xEA
	vic.rasterLine, vic.dot = 0x33, 20*8
	vic.control1, vic.allowBadLine = 0x13, true
	vic.memPointers = 0x10 // Screen at $0400.
	vic.syncLineVisibility()
	for i := 0; i < 40; i++ {
		ram[0x0400+i], colorRAM[i] = 0x42, 5
	}
	for n := 0; n < 4; n++ {
		vic.StepCycle()
		if vic.BA || vic.AEC != (n < 3) {
			t.Fatalf("late-DMA cycle %d: BA/AEC=%v/%v", n, vic.BA, vic.AEC)
		}
		want := uint16(0xFF) // Colour during warning is explicitly unsupported.
		if n == 3 {
			want = 0x542
		}
		if got := vic.videoMatrixColor[vic.VMLI]; got != want {
			t.Fatalf("late-DMA cycle %d: matrix=%03X, want %03X", n, got, want)
		}
	}
}

func TestVICWarningReleaseAndReset(t *testing.T) {
	newMachine(t)
	vic.dot, vic.rasterLine = 12, 0x33
	vic.allowBadLine, vic.control1 = true, 0x13
	for n := 0; n < 2; n++ {
		vic.phi0low()
		if vic.AEC {
			t.Fatal("AEC high during Phi1")
		}
		vic.phi0high()
		if !vic.AEC {
			t.Fatal("AEC low before warning expires")
		}
	}
	vic.control1 = 0x14 // Cancel the badline and reset the warning.
	vic.phi0low()
	vic.phi0high()
	if !vic.BA || !vic.AEC || vic.baLowCycles != 0 {
		t.Fatal("cancelled warning did not release the bus")
	}
	vic.control1 = 0x13
	for n := 0; n < 4; n++ {
		vic.phi0low()
		vic.phi0high()
		if vic.AEC != (n < 3) {
			t.Fatalf("restarted warning cycle %d: AEC=%v", n, vic.AEC)
		}
	}
	vic.Reset()
	if vic.baLowCycles != 0 || !vic.BA || !vic.AEC {
		t.Fatal("reset retained bus takeover state")
	}
}

func TestCPUWriteSequencesDuringBAWarning(t *testing.T) {
	for _, tc := range []struct {
		name      string
		opcode    uint8
		interrupt uint8
		first     uint8
		writes    int
	}{
		{"BRK", 0, 0, 2, 3},
		{"IRQ", 0, 1, 2, 3},
		{"NMI", 0, 2, 2, 3},
		{"JSR", 0x20, 0, 3, 2},
		{"PHA", 0x48, 0, 2, 1},
		{"ASL", 0x06, 0, 3, 2},
		{"INC", 0xEE, 0, 4, 2},
		{"STA", 0x8D, 0, 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newMachine(t)
			savedBus := bus
			t.Cleanup(func() { bus = savedBus })
			cpu.PortDDR, cpu.Port = 0xFF, 0
			cpu.PC, cpu.SP, cpu.A = 0x0200, 0xFF, 0x42
			copy(ram[0x0200:], []byte{tc.opcode, 0x10, 0x30, 0xEA})
			for n := 0; cpu.TState != tc.first; n++ {
				if n > 8 {
					t.Fatal("did not reach first write")
				}
				cpu.TickPhi2()
			}
			cpu.Interrupt = tc.interrupt
			vic.BA = false
			for n := 0; n < tc.writes; n++ {
				bus.RW = true
				before := cpu.TState
				cpu.TickPhi2()
				if bus.RW || cpu.TState == before {
					t.Fatalf("write %d did not complete during warning", n)
				}
			}
			before := cpu
			vic.AEC = false
			for n := 0; n < 4; n++ {
				cpu.TickPhi2()
			}
			before.Clock += 4
			if cpu != before {
				t.Fatal("CPU advanced past writes into held read")
			}
		})
	}
}

func TestAECOwnershipDoesNotActAsRDY(t *testing.T) {
	newMachine(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	cpu.PortDDR, cpu.Port = 0xFF, 7
	cpu.A, cpu.Opcode, cpu.TState = 0x42, 0x8D, 3
	vic.BA, vic.AEC = false, false
	for _, addr := range []uint16{0x0400, 0xD020, 0xDC0D} {
		cpu.Operand, cpu.TState = addr, 3
		beforeBus, beforeRAM := bus, ram
		border, mask := vic.borderColor, cia1.imr
		cpu.TickPhi2()
		if cpu.TState != 0 {
			t.Fatal("AEC incorrectly stalled a CPU write")
		}
		if bus != beforeBus || ram != beforeRAM || vic.borderColor != border || cia1.imr != mask {
			t.Fatalf("disconnected CPU wrote external address %04X", addr)
		}
	}
	cpu.store(0, 0xA5)
	cpu.store(1, 0x5A)
	if cpu.PortDDR != 0xA5 || cpu.Port != 0x5A {
		t.Fatal("AEC disabled the internal CPU port")
	}
	cpu.PortDDR, cpu.Port = 0xFF, 7
	cia1.icr = 1
	bus.Data = 0xEA
	if got := cpu.load(0xDC0D); got != 0xEA || cia1.icr != 1 {
		t.Fatal("disconnected CPU read had an I/O side effect")
	}
}

func TestCPUResetWhileBusDisconnected(t *testing.T) {
	newMachine(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	cpu.PortDDR, cpu.Port = 0xFF, 0
	ram[0xFFFC], ram[0xFFFD] = 0x34, 0x12
	vic.BA, vic.AEC = false, false
	cpu.Reset()
	if cpu.PC != 0x1234 || cpu.TState != 0 {
		t.Fatalf("synchronous reset sampled disconnected bus: PC=%04X T=%d", cpu.PC, cpu.TState)
	}
	if vic.BA || vic.AEC {
		t.Fatal("CPU-only reset altered VIC signals")
	}
}

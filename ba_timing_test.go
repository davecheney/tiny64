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
			for n := 0; n < 4; n++ {
				cpu.TickPhi2()
			}
			if cpu != before {
				t.Fatal("CPU advanced past writes into held read")
			}
		})
	}
}

// TestCPUIsOffTheBusBeforeAECDrops states the invariant that lets load and
// store ignore AEC entirely, on the emulator's hottest path.
//
// BA drives RDY, which halts the CPU on a read but lets an in-flight write
// finish, so while BA is low the CPU advances only as long as it has writes
// to retire. The VIC gives three cycles of warning before AEC drops. The
// longest run of consecutive writes any opcode can present is also three -
// BRK's three pushes - so the CPU is always parked on a held read by the
// time AEC goes low, and never performs a bus access while disconnected.
//
// The margin is exactly zero, in both directions. Widening a write run or
// shortening the warning would let a disconnected CPU read RAM it cannot
// see and write to addresses it cannot reach, and nothing else in the tree
// would notice. This is the test that would.
func TestCPUIsOffTheBusBeforeAECDrops(t *testing.T) {
	savedRAM, savedBus, savedCPU, savedVIC := ram, bus, cpu, vic
	t.Cleanup(func() { ram, bus, cpu, vic = savedRAM, savedBus, savedCPU, savedVIC })

	// Derive the runs from the microcode rather than from cpuWritesThisCycle,
	// so that adding a longer-writing instruction is caught even if the
	// decoder is faithfully updated to describe it.
	operands := [][4]uint8{
		{0x10, 0x30, 0x00, 0x00},
		{0x10, 0x30, 0x01, 0x01},
		{0xF0, 0x30, 0x04, 0x04},
		{0x20, 0x31, 0x10, 0x10},
	}
	longest, longestOp := 0, 0
	for op := 0; op < 256; op++ {
		for _, v := range operands {
			mask, ok := deriveWriteMask(uint8(op), v[0], v[1], v[2], v[3])
			if !ok {
				continue
			}
			run := 0
			for ts := 0; ts < 16; ts++ {
				if mask>>ts&1 == 0 {
					run = 0
					continue
				}
				if run++; run > longest {
					longest, longestOp = run, op
				}
			}
		}
	}

	// And the warning: cycles AEC stays high after BA falls.
	var v VICII
	v.BA = true
	v.phi0high()
	v.BA = false
	warning := 0
	for i := 0; i < 16; i++ {
		v.phi0high()
		if !v.AEC {
			break
		}
		warning++
	}

	if longest > warning {
		t.Errorf("opcode $%02X writes for %d consecutive cycles but AEC only warns for %d; "+
			"a disconnected CPU can now reach the bus, so load and store must test AEC again",
			longestOp, longest, warning)
	}
	t.Logf("longest write run %d cycles (opcode $%02X), AEC warning %d cycles", longest, longestOp, warning)
}

package tiny64

import "testing"

func TestVICStepCycleStalledReadClocksCIAs(t *testing.T) {
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	newMachine(t)
	iecBus = nil
	cpu.PC = 0x0200
	ram[0x0200] = 0xEA // NOP
	vic.rasterLine = 0x33
	vic.slot = 1
	vic.control1 = 0x13
	vic.allowBadLine = true
	vic.syncLineVisibility()

	for _, c := range []*chip{&cia.cia1, &cia.cia2} {
		c.store(0x04, 10, sourceCIA1)
		c.store(0x05, 0, sourceCIA1)
		c.store(0x0E, 1, sourceCIA1)
	}
	// The CIA timers are the cycle count. Both are clocked from
	// CPU.TickPhi2 and from nowhere else, above its stall return, so a
	// timer that has counted down by exactly n is proof that n StepCycles
	// ran exactly n CPU Phi2 cycles - even though the CPU itself made no
	// progress on any of them.
	for n := uint16(1); n <= 3; n++ {
		vic.StepCycle()
		if vic.BA || !vic.AEC() {
			t.Fatalf("cycle %d: expected BA warning with AEC still high", n)
		}
		if cpu.PC != 0x0200 || cpu.TState != 0 {
			t.Fatalf("cycle %d: stalled CPU advanced to PC=%04X T=%d", n, cpu.PC, cpu.TState)
		}
		if cia.cia1.timerA != 10-n || cia.cia2.timerA != 10-n {
			t.Fatalf("cycle %d: CIA timers=%d/%d, want %d/%d", n, cia.cia1.timerA, cia.cia2.timerA, 10-n, 10-n)
		}
	}
}

// TestVICStepCycleCPUWriteLandsMidSlot pins where the CPU's bus cycle sits
// inside the VIC's eight dots. VINC falls at dot 428, four dots into its
// own slot rather than on the boundary - so Phi2 falls halfway through the
// slot. The first four dots of the slot are painted before the
// CPU runs and the last four after it, which is what lets a $D016 write
// made in one slot be read by a border comparison in the next.
func TestVICStepCycleCPUWriteLandsMidSlot(t *testing.T) {
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	newMachine(t)
	iecBus = nil
	cpu.PC = 0x0200
	cpu.A = 5
	copy(ram[0x0200:], []byte{0x8D, 0x20, 0xD0, 0xEA}) // STA $D020; NOP
	vic.rasterLine = 100
	vic.slot = 2
	vic.borderColor = 2
	vic.syncLineVisibility()

	for cycle := 1; cycle <= 4; cycle++ {
		vic.StepCycle()
		want := uint8(2)
		if cycle == 4 {
			want = 5
		}
		if vic.borderColor != want {
			t.Fatalf("cycle %d: border color=%d, want %d", cycle, vic.borderColor, want)
		}
	}
	if vic.slot != 6 || bus.RW || bus.Address != 0xD020 || bus.Data != 5 {
		t.Fatalf("store did not complete in the slot ending at dot 48: dot=%d bus=%+v", vic.Dot(), bus)
	}
	for dot := uint16(40); dot <= 43; dot++ {
		if !frameBufferPixelIs(dot, 100, 2) {
			t.Fatalf("dot %d was painted with the new color before CPU Phi2", dot)
		}
	}
	for dot := uint16(44); dot <= 47; dot++ {
		if !frameBufferPixelIs(dot, 100, 5) {
			t.Fatalf("dot %d was painted with the old color after CPU Phi2", dot)
		}
	}

	vic.StepCycle()
	for dot := uint16(48); dot <= 55; dot++ {
		if !frameBufferPixelIs(dot, 100, 5) {
			t.Fatalf("dot %d did not use the color written in the previous cycle", dot)
		}
	}
}

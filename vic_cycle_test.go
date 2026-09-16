package tiny64

import "testing"

func TestVICStepCycleBadLineSuppressesCPUAndClocksCIAs(t *testing.T) {
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	newMachine(t)
	iecBus = nil
	cpu.PC = 0x0200
	ram[0x0200] = 0xEA // NOP
	vic.rasterLine = 0x33
	vic.dot = DotsPerCycle
	vic.control1 = 0x13
	vic.allowBadLine = true
	vic.syncLineVisibility()

	for _, c := range []*CIA{&cia1, &cia2} {
		c.Store(0x04, 10)
		c.Store(0x05, 0)
		c.Store(0x0E, 1)
	}
	// The CIA timers are the cycle count. Both are clocked from
	// CPU.TickPhi2 and from nowhere else, above its stall return, so a
	// timer that has counted down by exactly n is proof that n StepCycles
	// ran exactly n CPU Phi2 cycles - even though the CPU itself made no
	// progress on any of them.
	for n := uint16(1); n <= 3; n++ {
		vic.StepCycle()
		if cpu.PC != 0x0200 || cpu.TState != 0 {
			t.Fatalf("cycle %d: bad line advanced CPU to PC=%04X T=%d", n, cpu.PC, cpu.TState)
		}
		if cia1.timerA != 10-n || cia2.timerA != 10-n {
			t.Fatalf("cycle %d: CIA timers=%d/%d, want %d/%d", n, cia1.timerA, cia2.timerA, 10-n, 10-n)
		}
	}
	if cia1.timerA == 10 || cia2.timerA == 10 {
		t.Fatalf("CIAs did not advance during bad line: timers=%d/%d", cia1.timerA, cia2.timerA)
	}
}

func TestVICCPUWindowIncludesSpriteDMAReadLead(t *testing.T) {
	for _, tc := range []struct {
		name    string
		display uint8
		first   uint16
		last    uint16
	}{
		{"sprite 0", 0x01, 44, 48},
		{"sprite 7", 0x80, 58, 62},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := VICII{spriteDisplay: tc.display}
			for slot := uint16(0); slot < CyclesPerLine; slot++ {
				v.dot = (slot + 1) * DotsPerCycle
				if slot == CyclesPerLine-1 {
					v.dot = 0
				}
				if got, want := v.vicCPUWindow(), slot >= tc.first && slot <= tc.last; got != want {
					t.Errorf("slot %d: VIC CPU window=%v, want %v", slot, got, want)
				}
			}
		})
	}
}

func TestVICStepCycleCPUWriteFollowsPixels(t *testing.T) {
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	newMachine(t)
	iecBus = nil
	cpu.PC = 0x0200
	cpu.A = 5
	copy(ram[0x0200:], []byte{0x8D, 0x20, 0xD0, 0xEA}) // STA $D020; NOP
	vic.rasterLine = 100
	vic.dot = 16
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
	if vic.dot != 48 || bus.RW || bus.Address != 0xD020 || bus.Data != 5 {
		t.Fatalf("store did not complete at dot 48: dot=%d bus=%+v", vic.dot, bus)
	}
	for dot := uint16(41); dot <= 48; dot++ {
		if !frameBufferPixelIs(dot, 100, 2) {
			t.Fatalf("dot %d was painted with the new color before CPU Phi2", dot)
		}
	}

	vic.StepCycle()
	for dot := uint16(49); dot <= 56; dot++ {
		if !frameBufferPixelIs(dot, 100, 5) {
			t.Fatalf("dot %d did not use the color written in the previous cycle", dot)
		}
	}
}

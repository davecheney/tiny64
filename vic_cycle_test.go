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
	vic.dot = DotsPerCycle
	vic.control1 = 0x13
	vic.allowBadLine = true
	vic.syncLineVisibility()

	for _, c := range []*CIA{&cia1, &cia2} {
		c.Store(0x04, 10)
		c.Store(0x05, 0)
		c.Store(0x0E, 1)
	}
	startClock := cpu.Clock
	for n := uint64(1); n <= 3; n++ {
		vic.StepCycle()
		if vic.BA || vic.AEC {
			t.Fatalf("cycle %d: bad line did not take the bus", n)
		}
		if cpu.PC != 0x0200 || cpu.TState != 0 {
			t.Fatalf("cycle %d: stalled CPU advanced to PC=%04X T=%d", n, cpu.PC, cpu.TState)
		}
		if cpu.Clock != startClock+n || cia1.timerA != 10-uint16(n) || cia2.timerA != 10-uint16(n) {
			t.Fatalf("cycle %d: clock delta=%d CIA timers=%d/%d", n, cpu.Clock-startClock, cia1.timerA, cia2.timerA)
		}
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

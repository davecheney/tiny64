package tiny64

import "testing"

// TestCIATicksEveryBusCycle pins the invariant that motivates clocking the
// CIAs from the VIC-II rather than from the CPU: Phi2 does not stop when
// the VIC takes the bus, so neither do the CIA timers.
//
// The stalls are not rare. With the screen on, the 25 Bad Lines of a PAL
// frame hold the CPU off the bus for 43 cycles each - 1075 of the frame's
// 19656, or 5.5% - and eight active sprites add another 420. A model that
// clocked the CIAs from the CPU's own tick would drop every one of those,
// running the jiffy clock and every CIA-timed IRQ slow by that much, and
// by an amount that changes with how much screen and sprite DMA is on.
func TestCIATicksEveryBusCycle(t *testing.T) {
	m := newMachine(t)
	m.run(200_000)

	// Drive the video state directly rather than waiting for the KERNAL to
	// reach IOINIT: DEN off means no Bad Lines, and no Bad Lines means this
	// test silently stops covering the case it exists for.
	vic.WriteRegister(0xD011, 0x1B) // DEN | RSEL | YSCROLL=3
	vic.WriteRegister(0xD015, 0xFF) // all eight sprites...
	for s := range 8 {
		vic.WriteRegister(uint16(0xD001+2*s), uint8(60+s)) // ...inside the display window
	}
	// allowBadLine is latched from DEN once per frame on raster line $30,
	// so the write above only takes effect from the next frame.
	for range CyclesPerFrame {
		vic.StepCycle()
	}

	// CIA1 Timer A free-running off Phi2, with the longest latch available
	// so it cannot underflow and reload mid-probe.
	cia1.Store(0xDC0E, 0x00)
	cia1.Store(0xDC04, 0xFF)
	cia1.Store(0xDC05, 0xFF)
	cia1.Store(0xDC0E, 0x11) // LOAD | START
	cia1.Store(0xDC0E, 0x01) // START, continuous

	read := func() int {
		return int(cia1.Load(0xDC04)) | int(cia1.Load(0xDC05))<<8
	}
	before := read()

	const cycles = CyclesPerFrame
	var held, badLineHeld int
	for range cycles {
		// A hold freezes the instruction sequencer and nothing else, so
		// watch the T-state rather than re-deriving the hold condition
		// here: BA low stalls the CPU on a read but lets an in-flight
		// write finish, and only TickPhi2 knows which of the two it ran.
		tstate := cpu.TState
		vic.StepCycle()
		if cpu.TState == tstate {
			held++
			if vic.badLine {
				badLineHeld++
			}
		}
	}

	// Guard the guard: if the video state ever stops producing stalls this
	// test would pass while asserting nothing.
	if badLineHeld == 0 {
		t.Error("no CPU cycles were held for a Bad Line; the probe is not covering them")
	}
	if held-badLineHeld == 0 {
		t.Error("no CPU cycles were held for sprite DMA; the probe is not covering it")
	}
	if got := before - read(); got != cycles {
		t.Errorf("CIA1 Timer A advanced %d times over %d bus cycles (%d of them with the CPU held off the bus); want %d",
			got, cycles, held, cycles)
	}
}

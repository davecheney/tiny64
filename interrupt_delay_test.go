package tiny64

import (
	"math/rand"
	"testing"
)

// Preserve the absolute-clock model as an independent oracle for ordinary
// timing, including opcode-fetch sampling and reset's retained line history.
func TestInterruptDelayMatchesAbsoluteClock(t *testing.T) {
	for _, seed := range []int64{1, 57, 71} {
		newNMIFixture(t)
		rng := rand.New(rand.NewSource(seed))
		var clock, irqAt, nmiAt uint64
		var irqLine, nmiLine, nmiLatched bool
		for cycle := 0; cycle < 10000; cycle++ {
			if rng.Intn(17) == 0 {
				cia1.IRQ = !cia1.IRQ
			}
			if rng.Intn(19) == 0 {
				cia2.IRQ = !cia2.IRQ
			}
			if rng.Intn(13) == 0 {
				keyboard.Restore()
				nmiLatched, nmiAt = true, clock
			}
			if cycle%211 == 210 {
				cpu.Reset()
				cpu.PC = 0x0200
				nmiLatched = false
			}
			vic.AEC = rng.Intn(3) != 0
			cpu.effectiveI = uint8(rng.Intn(2)) * P_INTERRUPT
			poll := vic.AEC && cpu.TState == 0
			clock++
			want := uint8(0)
			if poll {
				if cia2.IRQ && !nmiLine {
					nmiLatched, nmiAt = true, clock
				}
				nmiLine = cia2.IRQ
				if cia1.IRQ && !irqLine {
					irqAt = clock
				}
				irqLine = cia1.IRQ
				switch {
				case nmiLatched && clock >= nmiAt+2:
					want, nmiLatched = 2, false
				case cia1.IRQ && cpu.effectiveI == 0 && clock >= irqAt+2:
					want = 1
				}
			}
			cpu.TickPhi2()
			if poll && cpu.Interrupt != want {
				t.Fatalf("seed=%d cycle=%d: entry=%d want=%d", seed, cycle, cpu.Interrupt, want)
			}
			if cpu.nmiLatch != nmiLatched || cpu.nmiLine != nmiLine || cpu.irqLine != irqLine {
				t.Fatalf("seed=%d cycle=%d: latch or edge history changed", seed, cycle)
			}
		}
	}
}

func TestInterruptDelaySurvivesClockWrap(t *testing.T) {
	for _, source := range []string{"IRQ", "CIA2", "RESTORE"} {
		for _, held := range []int{0, 1, 2, 4} {
			measure := func(start uint) []uint8 {
				newNMIFixture(t)
				cpu.Clock = start
				switch source {
				case "IRQ":
					cia1.IRQ = true
				case "CIA2":
					cia2.IRQ = true
				case "RESTORE":
					keyboard.Restore()
				}
				trace := make([]uint8, 20)
				for cycle := range trace {
					vic.AEC = cycle >= held
					if cycle == 1 && source == "RESTORE" {
						keyboard.Restore()
					}
					cpu.TickPhi2()
					trace[cycle] = cpu.Interrupt
				}
				return trace
			}
			want := measure(100)
			seen := false
			for _, interrupt := range want {
				seen = seen || interrupt != 0
			}
			if !seen {
				t.Fatal("reference trace never serviced an interrupt")
			}
			for _, start := range []uint{^uint(0) - 3, ^uint(0) - 2, ^uint(0) - 1, ^uint(0)} {
				got := measure(start)
				for cycle := range want {
					if got[cycle] != want[cycle] {
						t.Fatalf("%s held=%d start=%x cycle=%d: got=%d want=%d",
							source, held, start, cycle, got[cycle], want[cycle])
					}
				}
			}
		}
	}
}

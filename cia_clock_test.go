package tiny64

import "testing"

func TestCPUDoesNotClockCIAs(t *testing.T) {
	newNMIFixture(t)
	cia1 = CIA{timerA: 100, runningA: true}
	cia2 = CIA{timerA: 100, runningA: true}
	runCycles(10)
	if cia1.timerA != 100 || cia2.timerA != 100 {
		t.Fatal("CPU-only ticks advanced the CIAs")
	}
}

func TestCIASchedulerUnderflowOrder(t *testing.T) {
	newNMIFixture(t)
	iecBus = nil
	cia1 = CIA{timerA: 1, latchA: 0xFFFF, runningA: true, imr: 1}
	vic.StepCycle()
	if !cia1.IRQ || cpu.Interrupt != 0 || cpu.TState != 1 {
		t.Fatal("underflow was not deferred until after the CPU fetch")
	}
	for range 2 {
		vic.StepCycle()
	}
	if cpu.Interrupt != 0 || cpu.TState != 0 {
		t.Fatal("IRQ interrupted the in-flight JMP")
	}
	vic.StepCycle()
	if cpu.Interrupt != 1 {
		t.Fatal("IRQ was not serviced on the next opcode fetch")
	}
}

func TestCIATicksEveryBusCycle(t *testing.T) {
	for _, frame := range []bool{false, true} {
		newNMIFixture(t)
		vic.Reset()
		iecBus = nil
		vic.WriteRegister(0xD011, 0x1B)
		vic.allowBadLine = true
		for _, chip := range []*CIA{&cia1, &cia2} {
			chip.Store(4, 0xFF)
			chip.Store(5, 0xFF)
			chip.Store(0xE, 1)
		}
		if frame {
			StepFrame()
		} else {
			held := 0
			for range CyclesPerFrame {
				vic.StepCycle()
				if !vic.AEC {
					held++
				}
			}
			if held == 0 {
				t.Fatal("probe did not exercise CPU stalls")
			}
		}
		for _, chip := range []*CIA{&cia1, &cia2} {
			if got := 0xFFFF - chip.timerA; got != CyclesPerFrame {
				t.Fatalf("frame=%v: timer ticks=%d want=%d", frame, got, CyclesPerFrame)
			}
		}
	}
}

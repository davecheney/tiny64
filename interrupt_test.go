package tiny64

import "testing"

func TestInterruptSourceBitsAreIndependent(t *testing.T) {
	for mask := interruptSource(0); mask < 16; mask++ {
		for _, source := range []interruptSource{sourceVIC, sourceCIA1, sourceCIA2, sourceRESTORE} {
			for _, asserted := range []bool{false, true} {
				c := CPU{interruptSources: mask}
				c.setInterrupt(source, asserted)
				want := mask &^ source
				if asserted {
					want |= source
				}
				if c.interruptSources != want {
					t.Fatalf("mask=%04b source=%04b asserted=%t: got %04b, want %04b", mask, source, asserted, c.interruptSources, want)
				}
				if c.irqActive != (asserted && source&irqSources != 0) {
					t.Fatal("only an asserted IRQ source should wake IRQ processing")
				}
			}
		}
	}
}

func TestInterruptClockMatchesPolledSources(t *testing.T) {
	for bits := range 8 {
		initial := irqState{sampled: bits&1 != 0, pending: bits&2 != 0, held: bits&4 != 0}
		for mask := interruptSource(0); mask < 16; mask++ {
			for _, i := range []uint8{0, P_INTERRUPT} {
				for _, poll := range []bool{false, true} {
					for _, stalled := range []bool{false, true} {
						for _, wake := range []bool{false, true} {
							asserted := mask&irqSources != 0
							c := CPU{
								irq:              initial,
								interruptSources: mask,
								irqActive:        wake || asserted || initial.sampled || initial.held,
							}
							want := initial
							want.clock(asserted, i, poll, stalled)
							if c.irqActive {
								c.clockIRQ(i, poll, stalled)
							}
							if c.irq != want || c.irqActive != (want.sampled || want.held) {
								t.Fatalf("initial=%+v mask=%04b I=%d poll=%t stalled=%t wake=%t: got %+v active=%t, want %+v",
									initial, mask, i, poll, stalled, wake, c.irq, c.irqActive, want)
							}
						}
					}
				}
			}
		}
	}
}

func TestInterruptPeripheralNotifications(t *testing.T) {
	newIRQTestCPU(t, "6510")
	for _, c := range []*CIA{&cia1, &cia2} {
		c.Store(4, 1)
		c.Store(5, 0)
		c.Store(0xD, 0x81)
		c.Store(0xE, 0x19) // One-shot timer, started and force-loaded.
		c.Tick()
	}
	vic.checkRasterIRQ()
	vic.WriteRegister(0xD01A, 1)
	keyboard.Restore()
	if want := irqSources | nmiSources; cpu.interruptSources != want {
		t.Fatalf("asserted sources=%04b, want %04b", cpu.interruptSources, want)
	}
	if status := cia1.Load(0xD); status != 0x81 {
		t.Fatalf("CIA1 ICR=%02x, want 81", status)
	}
	if want := sourceVIC | nmiSources; cpu.interruptSources != want {
		t.Fatalf("CIA1 acknowledgement cleared other sources: %04b", cpu.interruptSources)
	}
	vic.WriteRegister(0xD01A, 0)
	if cpu.interruptSources != nmiSources {
		t.Fatal("disabling the VIC mask did not release only VIC IRQ")
	}
	vic.WriteRegister(0xD01A, 1)
	if cpu.interruptSources != sourceVIC|nmiSources {
		t.Fatal("enabling a pending VIC interrupt did not reassert its source")
	}
	vic.WriteRegister(0xD019, 1)
	cia2.Load(0xD)
	if cpu.interruptSources != sourceRESTORE {
		t.Fatalf("acknowledgements changed RESTORE: %04b", cpu.interruptSources)
	}
	keyboard.restore = 1
	keyboard.tick()
	if cpu.interruptSources != 0 {
		t.Fatal("expired RESTORE pulse did not release its source")
	}
	if !cpu.irqActive {
		t.Fatal("source release discarded the deferred IRQ clock")
	}
	cpu.clockIRQ(0, false, false)
	if cpu.irqActive {
		t.Fatal("drained IRQ state did not return to the inactive path")
	}
}

func TestInterruptCombinedNMIEdges(t *testing.T) {
	c := newIRQTestCPU(t, "6510")
	keyboard.Restore()
	c.tick()
	if !cpu.nmiLatch {
		t.Fatal("RESTORE did not produce an NMI edge")
	}
	cpu.nmiLatch = false
	cia2.setIRQ(true)
	keyboard.restore = 1
	c.tick()
	if cpu.nmiLatch || !cpu.nmiLine || cpu.interruptSources != sourceCIA2 {
		t.Fatal("source handoff changed the combined NMI level or created another edge")
	}
	cia2.Load(0xD)
	c.tick()
	if cpu.nmiLine {
		t.Fatal("combined NMI line did not release")
	}
	keyboard.Restore()
	c.tick()
	if !cpu.nmiLatch {
		t.Fatal("a new combined NMI edge was lost")
	}
}

func TestInterruptResetRetainsPeripheralLevels(t *testing.T) {
	newIRQTestCPU(t, "6510")
	vic.setIRQ(true)
	cia1.setIRQ(true)
	cia2.setIRQ(true)
	keyboard.Restore()
	cpu.irq = irqState{sampled: true, pending: true, held: true}
	cpu.nmiLine = true
	cpu.Reset()
	if cpu.interruptSources != irqSources|nmiSources || !cpu.irqActive || cpu.irq != (irqState{}) || !cpu.nmiLine {
		t.Fatal("CPU reset did not preserve peripheral levels and discard IRQ history")
	}
	vic.Reset()
	if cpu.interruptSources != sourceCIA1|nmiSources {
		t.Fatal("VIC reset changed another device's interrupt output")
	}
}

func TestInterruptUnconnectedPeripherals(t *testing.T) {
	newIRQTestCPU(t, "6510")
	var c CIA
	var v VICII
	var k Keyboard
	c.setIRQ(true)
	v.setIRQ(true)
	k.Restore()
	if cpu.interruptSources != 0 || cpu.irqActive {
		t.Fatal("a standalone peripheral changed the machine's interrupt pins")
	}
}

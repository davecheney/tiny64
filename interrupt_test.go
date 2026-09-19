package tiny64

import "testing"

func TestInterruptSourceBitsAreIndependent(t *testing.T) {
	for mask := interruptSource(0); mask < 16; mask++ {
		for _, source := range []interruptSource{sourceVIC, sourceCIA1, sourceCIA2} {
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

// TestInterruptPeripheralNotifications exercises the four peripherals that
// still own bits in interruptSources: VIC and CIA1 assert IRQ, CIA2 asserts
// NMI, each independently acknowledged. RESTORE does not appear here: it
// bypasses this bitmask entirely and triggers the CPU's NMI latch
// directly, covered separately by TestInterruptCombinedNMIEdges and the
// RESTORE tests in 6510_nmi_test.go.
func TestInterruptPeripheralNotifications(t *testing.T) {
	newIRQTestCPU(t)
	for _, w := range []struct {
		c      *chip
		source interruptSource
	}{{&cia.cia1, sourceCIA1}, {&cia.cia2, sourceCIA2}} {
		w.c.store(4, 1, w.source)
		w.c.store(5, 0, w.source)
		w.c.store(0xD, 0x81, w.source)
		w.c.store(0xE, 0x19, w.source) // One-shot timer, started and force-loaded.
		w.c.tick(w.source)
	}
	vic.checkRasterIRQ()
	vic.WriteRegister(0xD01A, 1)
	if want := irqSources | nmiSources; cpu.interruptSources != want {
		t.Fatalf("asserted sources=%03b, want %03b", cpu.interruptSources, want)
	}
	if status := cia.cia1.load(0xD, sourceCIA1); status != 0x81 {
		t.Fatalf("CIA1 ICR=%02x, want 81", status)
	}
	if want := sourceVIC | nmiSources; cpu.interruptSources != want {
		t.Fatalf("CIA1 acknowledgement cleared other sources: %03b", cpu.interruptSources)
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
	cia.cia2.load(0xD, sourceCIA2)
	if cpu.interruptSources != 0 {
		t.Fatalf("acknowledgements left a stale source: %03b", cpu.interruptSources)
	}
	if !cpu.irqActive {
		t.Fatal("source release discarded the deferred IRQ clock")
	}
	cpu.clockIRQ(0, false, false)
	if cpu.irqActive {
		t.Fatal("drained IRQ state did not return to the inactive path")
	}
}

// TestInterruptCombinedNMIEdges checks that RESTORE's direct trigger and
// CIA2's source-bitmask edge detection coexist without interfering: a
// RESTORE press always latches a fresh NMI, while CIA2 continues to be
// recognized only on its own 0->1 transition and to release cleanly on
// acknowledgement.
func TestInterruptCombinedNMIEdges(t *testing.T) {
	c := newIRQTestCPU(t)

	keyboard.Restore()
	c.tick()
	if !cpu.nmiLatch {
		t.Fatal("RESTORE did not produce an NMI edge")
	}
	cpu.nmiLatch = false

	cia.cia2.setIRQ(sourceCIA2, true)
	c.tick()
	if !cpu.nmiLatch || cpu.interruptSources != sourceCIA2 {
		t.Fatal("CIA2 did not produce its own NMI edge through the source bitmask")
	}
	cpu.nmiLatch = false

	c.tick() // CIA2's line is still held: this must not re-fire.
	if cpu.nmiLatch {
		t.Fatal("a held CIA2 line produced a second edge")
	}

	cia.cia2.load(0xD, sourceCIA2)
	c.tick()
	if cpu.nmiLine {
		t.Fatal("CIA2's NMI line did not release after acknowledgement")
	}

	keyboard.Restore()
	c.tick()
	if !cpu.nmiLatch {
		t.Fatal("a fresh RESTORE press was lost")
	}
}

func TestInterruptResetRetainsPeripheralLevels(t *testing.T) {
	newIRQTestCPU(t)
	vic.setIRQ(true)
	cia.cia1.setIRQ(sourceCIA1, true)
	cia.cia2.setIRQ(sourceCIA2, true)
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

// TestInterruptUnconnectedPeripherals covers the two peripherals a caller
// can hold detached from the machine. chip is not one of them: it is
// unexported and exists only as a field of CIA.
func TestInterruptUnconnectedPeripherals(t *testing.T) {
	newIRQTestCPU(t)
	var v VICII
	var k Keyboard
	v.setIRQ(true)
	k.Restore()
	if cpu.interruptSources != 0 || cpu.irqActive {
		t.Fatal("a standalone peripheral changed the machine's interrupt pins")
	}
}

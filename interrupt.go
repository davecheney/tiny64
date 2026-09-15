package tiny64

type interruptSource uint8

const (
	sourceVIC interruptSource = 1 << iota
	sourceCIA1
	sourceCIA2

	irqSources = sourceVIC | sourceCIA1
	nmiSources = sourceCIA2
)

// Keep rare source transitions out of the per-cycle peripheral code.
//
//go:noinline
func (c *CPU) setInterrupt(source interruptSource, asserted bool) {
	if asserted {
		c.interruptSources |= source
		if source&irqSources != 0 {
			c.irqActive = true
		}
	} else {
		c.interruptSources &^= source
	}
}

// syncInterruptSources reconnects the existing levels without changing NMI edge
// history. Clearing a source must not discard a sample from the preceding Phi2.
//
// RESTORE is not one of these sources: it has no level of its own to
// resync, since it latches the CPU's NMI directly on its press edge
// instead of holding a line.
func (c *CPU) syncInterruptSources() {
	c.interruptSources = 0
	c.irqActive = false
	c.setInterrupt(sourceVIC, vic.IRQ)
	c.setInterrupt(sourceCIA1, cia1.IRQ)
	c.setInterrupt(sourceCIA2, cia2.IRQ)
}

func (c *CPU) clockIRQ(i uint8, poll, stalled bool) {
	asserted := c.interruptSources&irqSources != 0
	c.irq.clock(asserted, i, poll, stalled)
	// Accepted requests are checked at opcode fetch even after this path sleeps.
	c.irqActive = asserted || c.irq.held
}

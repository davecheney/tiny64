package tiny64

// CIA emulates a MOS 6526 Complex Interface Adapter. Both CIA1 (keyboard/
// joystick, IRQ) and CIA2 (serial bus/user port/VIC bank, NMI) share this
// implementation; only their memory address range and interrupt line
// differ. The Time-of-Day clock and serial shift register are not
// implemented.
type CIA struct {
	PRA, PRB   uint8 // Port A/B output latches
	DDRA, DDRB uint8 // Port A/B data direction (1 = output)

	timerA, timerB     uint16 // current countdown value
	latchA, latchB     uint16 // reload value, loaded from $x4-$x7
	runningA, runningB bool   // Timer A/B started (CRx bit 0)
	oneShotA, oneShotB bool   // stop on underflow instead of reloading (CRx bit 3)

	icr uint8 // latched interrupt flags; bit 7 set once any unmasked flag fires
	imr uint8 // interrupt mask (which flags in icr can assert IRQ)

	// IRQ is the chip's interrupt output line: asserted on an unmasked
	// timer underflow, and cleared when the ICR is read.
	IRQ bool
}

var cia1, cia2 CIA

// CIA1 returns the singleton CIA1 (keyboard/joystick; drives the CPU's IRQ
// line).
func CIA1() *CIA { return &cia1 }

// CIA2 returns the singleton CIA2 (serial bus/user port/VIC bank; drives
// the CPU's NMI line).
func CIA2() *CIA { return &cia2 }

// effective returns the electrical state of a CIA port: output-configured
// bits (ddr=1) reflect the written value, input-configured bits float high
// since no keyboard/joystick/serial device is modeled yet.
func effective(data, ddr uint8) uint8 {
	return (data & ddr) | ^ddr
}

// Tick advances both timers by one Phi2 cycle, latching an interrupt flag
// and asserting IRQ (if unmasked) on underflow. Only "count Phi2 pulses"
// mode is implemented; CNT-pin and timer-A-cascade modes are not.
func (c *CIA) Tick() {
	if c.runningA {
		if c.timerA > 0 {
			c.timerA--
		}
		if c.timerA == 0 {
			c.icr |= 0x01
			if c.oneShotA {
				c.runningA = false
			}
			c.timerA = c.latchA
			c.checkIRQ()
		}
	}

	if c.runningB {
		if c.timerB > 0 {
			c.timerB--
		}
		if c.timerB == 0 {
			c.icr |= 0x02
			if c.oneShotB {
				c.runningB = false
			}
			c.timerB = c.latchB
			c.checkIRQ()
		}
	}
}

// Only underflows and mask writes can make a latched source eligible.
func (c *CIA) checkIRQ() {
	if c.icr&c.imr&0x1F != 0 {
		c.icr |= 0x80
		c.IRQ = true
	}
}

// Load reads a CIA register, mirrored every 16 bytes.
func (c *CIA) Load(addr uint16) uint8 {
	switch addr & 0x0F {
	case 0x0:
		return effective(c.PRA, c.DDRA)
	case 0x1:
		return effective(c.PRB, c.DDRB)
	case 0x2:
		return c.DDRA
	case 0x3:
		return c.DDRB
	case 0x4:
		return uint8(c.timerA)
	case 0x5:
		return uint8(c.timerA >> 8)
	case 0x6:
		return uint8(c.timerB)
	case 0x7:
		return uint8(c.timerB >> 8)
	case 0xD:
		// Reading the ICR returns the latched flags and clears them (and
		// the IRQ line).
		v := c.icr
		c.icr = 0
		c.IRQ = false
		return v
	case 0xE:
		var cra uint8
		if c.runningA {
			cra |= 0x01
		}
		if c.oneShotA {
			cra |= 0x08
		}
		return cra
	case 0xF:
		var crb uint8
		if c.runningB {
			crb |= 0x01
		}
		if c.oneShotB {
			crb |= 0x08
		}
		return crb
	default:
		return 0 // TOD/SDR: not implemented
	}
}

// Store writes a CIA register, mirrored every 16 bytes.
func (c *CIA) Store(addr uint16, val uint8) {
	switch addr & 0x0F {
	case 0x0:
		c.PRA = val
	case 0x1:
		c.PRB = val
	case 0x2:
		c.DDRA = val
	case 0x3:
		c.DDRB = val
	case 0x4:
		c.latchA = c.latchA&0xFF00 | uint16(val)
	case 0x5:
		c.latchA = c.latchA&0x00FF | uint16(val)<<8
		if !c.runningA {
			c.timerA = c.latchA
		}
	case 0x6:
		c.latchB = c.latchB&0xFF00 | uint16(val)
	case 0x7:
		c.latchB = c.latchB&0x00FF | uint16(val)<<8
		if !c.runningB {
			c.timerB = c.latchB
		}
	case 0xD:
		// Bit 7 selects whether the low 5 bits set or clear the mask.
		if val&0x80 != 0 {
			c.imr |= val & 0x1F
		} else {
			c.imr &^= val & 0x1F
		}
		c.checkIRQ()
	case 0xE:
		c.runningA = val&0x01 != 0
		c.oneShotA = val&0x08 != 0
		if val&0x10 != 0 { // force load (self-clearing strobe)
			c.timerA = c.latchA
		}
	case 0xF:
		c.runningB = val&0x01 != 0
		c.oneShotB = val&0x08 != 0
		if val&0x10 != 0 {
			c.timerB = c.latchB
		}
	}
}

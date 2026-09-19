package tiny64

// CIA emulates a MOS 6526 Complex Interface Adapter. Both CIA1 (keyboard/
// joystick, IRQ) and CIA2 (serial bus/user port/VIC bank, NMI) share this
// implementation; only their memory address range and interrupt line
// differ. The Time-of-Day clock and serial shift register are not
// implemented.
type chip struct {
	PRA, PRB   uint8 // Port A/B output latches
	DDRA, DDRB uint8 // Port A/B data direction (1 = output)

	timerA, timerB uint16 // current countdown value
	latchA, latchB uint16 // reload value, loaded from $x4-$x7

	// running holds both timers' START bits, so tick can ask whether either
	// is counting with one compare against zero. Read and write it through
	// startA and startB.
	running uint8

	oneShotA, oneShotB bool // stop on underflow instead of reloading (CRx bit 3)

	icr uint8 // latched interrupt flags; bit 7 set once any unmasked flag fires
	imr uint8 // interrupt mask (which flags in icr can assert IRQ)

	// IRQ reports the chip's interrupt output line: asserted on an unmasked
	// timer underflow, and cleared when the ICR is read.
	// Treat it as read-only; setIRQ also updates the connected CPU pin.
	IRQ bool
}

// CIA is the pair of 6526s as the board wires them, not one chip twice.
// They share a timer core and an interrupt latch, and share nothing else:
// CIA1's port A scans the keyboard and its interrupt output drives IRQ,
// while CIA2's port A carries the VIC-II's two bank-select lines and the
// serial bus, and its output drives NMI.
//
// Holding both here is what lets that wiring be static. Callers name the
// chip they mean and hand the shared core the constant that goes with it,
// so nothing is decided at run time.
type CIA struct {
	cia1, cia2 chip
}

var cia CIA

// startA and startB are the bits of chip.running: each timer's START bit,
// CRA bit 0 and CRB bit 0. They sit at the bit positions of the ICR flags
// their underflows latch, so a timer and its flag share a bit number.
const (
	startA = 1 << iota
	startB
)

// setIRQ drives the chip's interrupt output. Which CPU pin that reaches -
// IRQ for CIA1, NMI for CIA2 - is the caller's constant. It needs no guard
// for a chip off the machine, as VICII.setIRQ and Keyboard.Restore do:
// chip is unexported and lives only as a field of CIA, so every chip that
// exists is one of the machine's two.
func (p *chip) setIRQ(source interruptSource, asserted bool) {
	p.IRQ = asserted
	cpu.setInterrupt(source, asserted)
}

// effective returns the electrical state of a CIA port: output-configured
// bits (ddr=1) reflect the written value, input-configured bits float high
// since no keyboard/joystick/serial device is modeled yet.
func effective(data, ddr uint8) uint8 {
	return (data & ddr) | ^ddr
}

// tick advances one chip's timers by one Phi2 cycle, latching an
// interrupt flag and asserting the chip's line (if unmasked) on underflow.
// Only "count Phi2 pulses" mode is implemented; CNT-pin and
// timer-A-cascade modes are not.
//
// Both chips are ticked from the VIC-II's cycle scheduler, not from the
// CPU, because that is where the clock physically comes from: the VIC-II
// divides the dot clock down to Phi2 and drives it out to the CPU and to
// both CIAs in parallel. Nothing the CPU does can gate it - not RDY, not
// AEC - so a CPU that is stalled off the bus for a Bad Line still sees
// the jiffy clock advance underneath it, exactly as the hardware does.
//
// Clocking the CIAs from inside CPU.TickPhi2 (where this used to live)
// made that independence an accident of where the RDY early-return
// happened to sit: moving the return above them would have cost the
// timers every stalled cycle, which with the screen on is the 43 cycles
// of each of a frame's 25 Bad Lines - 1075 of 19656, or 5.5% - plus up to
// 420 more for sprite DMA. Hanging the CIAs off the VIC makes the
// independence structural instead. TestCIATicksEveryBusCycle pins it.
//
// A chip with both timers stopped has nothing to count, and that is the
// common case: CIA2 all the time, and CIA1 until the KERNAL starts the
// jiffy clock. The test for it is here and the counting is out of line
// behind it, so tick fits the inliner's budget and the stopped case costs
// its caller a load and a compare with no call at all. The VIC-II's cycle
// bodies therefore tick each chip by name rather than through a wrapper
// over both, which could not fit that budget.
func (p *chip) tick(source interruptSource) {
	if p.running != 0 {
		p.count(source)
	}
}

// count counts one chip's timers down by one Phi2 cycle. tick has already
// established that at least one of them is running.
//
// The underflow handling is out of line, and noinline because the compiler
// will otherwise pull it back in: inlining the two underflow bodies here
// costs 156 of the inliner's 80-point budget and keeps count itself out of
// its caller. The cold path evicts the hot one.
func (p *chip) count(source interruptSource) {
	if p.running&startA != 0 {
		if p.timerA > 0 {
			p.timerA--
		}
		if p.timerA == 0 {
			p.underflowA(source)
		}
	}

	if p.running&startB != 0 {
		if p.timerB > 0 {
			p.timerB--
		}
		if p.timerB == 0 {
			p.underflowB(source)
		}
	}
}

//go:noinline
func (p *chip) underflowA(source interruptSource) {
	p.icr |= 0x01
	if p.oneShotA {
		p.running &^= startA
	}
	p.timerA = p.latchA
	p.checkIRQ(source)
}

//go:noinline
func (p *chip) underflowB(source interruptSource) {
	p.icr |= 0x02
	if p.oneShotB {
		p.running &^= startB
	}
	p.timerB = p.latchB
	p.checkIRQ(source)
}

// checkIRQ sets the master interrupt flag and asserts the chip's interrupt
// line if any enabled source has fired.
//
// Only icr and imr feed the test, so it runs at exactly the points that
// can newly satisfy it: a Timer A or Timer B underflow latching a flag
// into icr, and a CPU write to the mask register enabling a flag that is
// already latched. Reading the ICR is the only other writer of either
// field, and it zeroes icr and drops the line itself, which can only
// falsify the test - never satisfy it - so it needs no check of its own.
func (p *chip) checkIRQ(source interruptSource) {
	if p.icr&p.imr&0x1F != 0 {
		p.icr |= 0x80
		if !p.IRQ {
			p.setIRQ(source, true)
		}
	}
}

// load reads a CIA register, mirrored every 16 bytes.
func (p *chip) load(addr uint16, source interruptSource) uint8 {
	switch addr & 0x0F {
	case 0x0:
		return effective(p.PRA, p.DDRA)
	case 0x1:
		return effective(p.PRB, p.DDRB)
	case 0x2:
		return p.DDRA
	case 0x3:
		return p.DDRB
	case 0x4:
		return uint8(p.timerA)
	case 0x5:
		return uint8(p.timerA >> 8)
	case 0x6:
		return uint8(p.timerB)
	case 0x7:
		return uint8(p.timerB >> 8)
	case 0xD:
		// Reading the ICR returns the latched flags and clears them (and
		// the IRQ line).
		v := p.icr
		p.icr = 0
		p.setIRQ(source, false)
		return v
	case 0xE:
		var cra uint8
		if p.running&startA != 0 {
			cra |= 0x01
		}
		if p.oneShotA {
			cra |= 0x08
		}
		return cra
	case 0xF:
		var crb uint8
		if p.running&startB != 0 {
			crb |= 0x01
		}
		if p.oneShotB {
			crb |= 0x08
		}
		return crb
	default:
		return 0 // TOD/SDR: not implemented
	}
}

// store writes a CIA register, mirrored every 16 bytes.
func (p *chip) store(addr uint16, val uint8, source interruptSource) {
	switch addr & 0x0F {
	case 0x0:
		p.PRA = val
	case 0x1:
		p.PRB = val
	case 0x2:
		p.DDRA = val
	case 0x3:
		p.DDRB = val
	case 0x4:
		p.latchA = p.latchA&0xFF00 | uint16(val)
	case 0x5:
		p.latchA = p.latchA&0x00FF | uint16(val)<<8
		if p.running&startA == 0 {
			p.timerA = p.latchA
		}
	case 0x6:
		p.latchB = p.latchB&0xFF00 | uint16(val)
	case 0x7:
		p.latchB = p.latchB&0x00FF | uint16(val)<<8
		if p.running&startB == 0 {
			p.timerB = p.latchB
		}
	case 0xD:
		// Bit 7 selects whether the low 5 bits set or clear the mask.
		if val&0x80 != 0 {
			p.imr |= val & 0x1F
		} else {
			p.imr &^= val & 0x1F
		}
		// Unmasking a flag that already fired asserts the line; masking
		// one off cannot, but checkIRQ is a no-op in that direction.
		p.checkIRQ(source)
	case 0xE:
		// CRA bit 0 is START, and startA is that same bit, so the mask
		// takes it straight across.
		p.running = p.running&^startA | val&startA
		p.oneShotA = val&0x08 != 0
		if val&0x10 != 0 { // force load (self-clearing strobe)
			p.timerA = p.latchA
		}
	case 0xF:
		// CRB bit 0 is START, but startB is bit 1, so it shifts up one.
		p.running = p.running&^startB | val&0x01<<1
		p.oneShotB = val&0x08 != 0
		if val&0x10 != 0 {
			p.timerB = p.latchB
		}
	}
}

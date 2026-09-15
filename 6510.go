package tiny64

import "fmt"

const (
	P_SIGN      = 0x80
	P_OVERFLOW  = 0x40
	P_UNUSED    = 0x20
	P_BREAK     = 0x10
	P_DECIMAL   = 0x08
	P_INTERRUPT = 0x04
	P_ZERO      = 0x02
	P_CARRY     = 0x01
)

type CPU struct {
	A, X, Y uint8
	PC      uint16
	SP      uint8
	regP    uint8

	Opcode  uint8
	TState  uint8  // Current T-state (0, 1, 2...)
	Operand uint16 // Internal buffer for building addresses
	Pointer uint8  // Internal buffer for a zero-page base address (BAL)
	Value   uint8  // Internal buffer for read-modify-write operations
	Addr2   uint16 // Internal buffer for a second address (e.g. page-crossing fixups)

	// Interrupt records which kind of interrupt (if any) BRK's microcode is
	// currently servicing: 0 = a real BRK instruction, 1 = IRQ, 2 = NMI.
	Interrupt uint8
	nmiLine   bool // last-seen state of the CPU's NMI pin, for edge detection
	nmiLatch  bool // latched by a 0->1 transition of nmiLine, until serviced

	// Clock counts elapsed Phi2 cycles, including cycles held by RDY.
	// The existing NMI recognition model uses it; IRQ uses clocked state.
	Clock         uint64
	nmiLatchClock uint64
	irq           irqState

	// Port and PortDDR implement the 6510's on-chip I/O port at $0001/$0000,
	// a feature the plain 6502 does not have.
	Port    uint8
	PortDDR uint8

	// DecimalADCCount counts every ADC/SBC executed while the Decimal flag
	// is set; LastDecimalADCPC records the PC of the most recent one. adc()
	// only implements binary mode, so these help detect whether decimal
	// mode is ever actually exercised (and thus computing wrong results).
	DecimalADCCount  int
	LastDecimalADCPC uint16
}

var cpu CPU

// GetCPU returns the singleton CPU instance, for debugging/tracing tools.
func GetCPU() *CPU {
	return &cpu
}

// Status returns the processor status register (NV-BDIZC).
func (c *CPU) Status() uint8 {
	return c.regP
}

// Reset loads PC from the reset vector at $FFFC/$FFFD, as the real 6510
// does when the RESET line is asserted, and resets the VIC-II's internal
// video logic state.
func Reset() {
	cpu.Reset()
	vic.Reset()
	if driveAttached {
		ResetDrive()
	}
}

func (c *CPU) Reset() {
	// Restart the instruction sequencer. Loading the PC is not enough on
	// its own: T-states other than 0 are the middle of an instruction, so
	// a CPU reset part way through one would carry on running the rest of
	// that instruction's microcode against the new PC and then fetch
	// whatever it happened to land on. Real hardware abandons the
	// instruction and begins with an opcode fetch.
	c.TState = 0
	c.Opcode = 0
	c.Operand = 0
	c.Pointer = 0
	c.Value = 0
	c.Addr2 = 0

	// Reset masks interrupts and abandons any it was in the middle of
	// taking. The three stack slots come from the reset sequence going
	// through BRK's motions with the write line held to read: nothing is
	// stored, but the stack pointer still moves.
	c.Interrupt = 0
	c.nmiLatch = false
	c.regP |= P_INTERRUPT
	c.irq = irqState{}
	c.SP -= 3

	lo := c.load(0xFFFC)
	hi := c.load(0xFFFD)
	c.PC = uint16(hi)<<8 | uint16(lo)
}

// bankBits returns the effective (DDR-masked) state of the LORAM/HIRAM/
// CHAREN lines driven by the CPU's I/O port at $0001: pins configured as
// inputs float high, pins configured as outputs reflect the written value.
// These select the ROM/RAM/I-O banking performed by the PLA.
func (c *CPU) bankBits() (loram, hiram, charen bool) {
	effective := (c.Port & c.PortDDR) | ^c.PortDDR
	return effective&0x01 != 0, effective&0x02 != 0, effective&0x04 != 0
}

// load performs a Phi2 bus read cycle at addr, delegating to the bus.
// $0000/$0001 are the CPU's own I/O port, not RAM, and never touch the bus.
func (c *CPU) load(addr uint16) uint8 {
	switch addr {
	case 0x0000:
		return c.PortDDR
	case 0x0001:
		return (c.Port & c.PortDDR) | ^c.PortDDR // input pins float high
	default:
		return bus.Load(addr)
	}
}

// store performs a Phi2 bus write cycle at addr, delegating to the bus.
// $0000/$0001 are the CPU's own I/O port, not RAM, and never touch the bus.
func (c *CPU) store(addr uint16, val uint8) {
	switch addr {
	case 0x0000:
		c.PortDDR = val
	case 0x0001:
		c.Port = val
	default:
		bus.Store(addr, val)
	}
}

// push writes val to the hardware stack at $0100+SP, then decrements SP.
func (c *CPU) push(val uint8) {
	c.store(0x0100+uint16(c.SP), val)
	c.SP--
}

// pop reads the byte at the current $0100+SP, then increments SP, matching
// the 6502's "read old top of stack, then increment" pull microcode. The
// final read of a multi-byte pull (PLA/PLP's real value, RTI/RTS's last
// byte) doesn't increment again, so it uses a plain load instead.
func (c *CPU) pop() uint8 {
	val := c.load(0x0100 + uint16(c.SP))
	c.SP++
	return val
}

// TickPhi2 executes exactly one high-clock phase of the CPU. The CIAs are
// clocked from here too, since on real hardware they share the system
// Phi2 clock with the CPU (not the VIC-II's dot clock) and keep counting
// regardless of whether the CPU itself is stalled by BA/AEC.
func (c *CPU) TickPhi2() {
	c.Clock++

	cia1.Tick()
	cia2.Tick()
	// The key matrix is combinational and needs no clock, but the RESTORE
	// monostable is a timer, so it counts here with the CIAs. It isn't on
	// the bus, so AEC is none of its business.
	keyboard.tick()

	nmi := nmiAsserted()
	if nmi && !c.nmiLine {
		c.nmiLatch = true
		c.nmiLatchClock = c.Clock
	}
	c.nmiLine = nmi

	opcode, tstate, i := c.Opcode, c.TState, c.regP&P_INTERRUPT

	// BA low does not freeze the CPU outright. It drives the 6510's RDY
	// pin, which only halts the processor on a *read* cycle; a write cycle
	// already in flight always completes. That is precisely why the VIC-II
	// asserts BA three cycles before it actually takes the bus (see
	// spriteBASlotMask and the Bad Line c-access lead): the longest run of
	// consecutive writes a 6502 can perform is three - the interrupt
	// sequence's three pushes - so by the time AEC drops and the VIC owns
	// the bus, the CPU is guaranteed to have stopped.
	//
	// Modelling this matters for cycle-exact code: stalling unconditionally
	// halts the CPU up to three cycles early, and how early depends on
	// whatever instruction happens to be executing, which shows up as
	// timing jitter in raster code that reprograms sprites mid-screen.
	if !vic.AEC && cpuWriteCycles[c.Opcode]>>c.TState&1 == 0 {
		c.irq.clock(cia1.IRQ || vic.IRQ, i, false, true)
		// CLI/SEI's I update is not held by RDY: their first terminal
		// cycle polls with old I, later repetitions see the new value.
		// PLP differs: it needs the completing stack read to restore P.
		if c.TState == 1 {
			switch c.Opcode {
			case 0x58:
				c.regP &^= P_INTERRUPT
			case 0x78:
				c.regP |= P_INTERRUPT
			}
		}
		return
	}

	switch c.TState {
	// T0: Fetch the opcode, unless a pending interrupt takes over instead.
	// NMI is edge-triggered (CIA2 and the RESTORE monostable, latched);
	// IRQ acceptance was latched by the preceding instruction's poll.
	// Both are serviced via BRK's microcode.
	case 0:
		switch {
		case c.nmiLatch && c.Clock >= c.nmiLatchClock+2:
			// NMI is checked before IRQ because it wins when both are
			// pending, and it is deliberately not gated on the I flag: the
			// I flag masks IRQ only, which is what makes this interrupt
			// non-maskable.
			//
			// Recognition consumes the latched edge. That is the whole
			// point of latching it: the NMI pin is edge-triggered, so a
			// source that holds it asserted (CIA2 with an unserviced
			// interrupt, say) produces exactly one NMI rather than a new
			// one every instruction. Only a fresh 0->1 transition of
			// nmiLine sets the latch again, and that can't happen until
			// the pin has been seen deasserted in between.
			c.nmiLatch = false
			c.Interrupt = 2
			c.Opcode = 0x00
			c.TState = 1
		case c.irq.pending:
			c.load(c.PC) // Discarded opcode fetch; PC must not advance.
			c.Interrupt = 1
			c.Opcode = 0x00
			c.TState = 1
		default:
			c.Interrupt = 0
			c.Opcode = c.load(c.PC)
			c.PC++
			c.TState = 1
		}
		c.irq.pending = false // NMI also discards an accepted IRQ.
	case 1:
		// T1: Execute the instruction based on the opcode
		switch c.Opcode {
		case 0x00: // BRK/IRQ/NMI
			// Real 6502 reads and discards a "signature" byte here. A real
			// BRK instruction advances PC past it; a hardware interrupt
			// does not, since PC must resume at the interrupted instruction.
			c.load(c.PC)
			if c.Interrupt == 0 {
				c.PC++
			}
			c.TState = 2
		case 0x01: // ORA (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0x05: // ORA Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x06: // ASL Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x08: // PHP
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.TState = 2
		case 0x09: // ORA Immediate
			c.A |= c.load(c.PC)
			c.setNZ(c.A)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0A: // ASL Accumulator
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.setCarry(c.A&0x80 != 0) // Carry from old bit 7
			c.A <<= 1
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0B: // ANC Immediate (illegal opcode)
			c.A &= c.load(c.PC)
			c.setNZ(c.A)
			c.setCarry(c.A&0x80 != 0) // Carry = bit 7 of result (same as N)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0C: // NOP Absolute (illegal opcode / TOP)
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x0D: // ORA Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x0E: // ASL Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x10: // BPL
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x80 == 0 { // N clear: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0x11: // ORA (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0x15: // ORA Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x16: // ASL Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x18: // CLC
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.regP &^= 0x01 // Clear Carry flag
			c.TState = 0    // Finished, next cycle is T0 for next opcode
		case 0x19: // ORA Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x1D: // ORA Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x1E: // ASL Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x20: // JSR
			c.Operand = uint16(c.load(c.PC)) // Store target address low byte
			c.PC++
			c.TState = 2
		case 0x21: // AND (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0x24: // BIT Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x25: // AND Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x26: // ROL Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x28: // PLP
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.TState = 2
		case 0x29: // AND Immediate
			c.A &= c.load(c.PC)
			c.setNZ(c.A)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2A: // ROL Accumulator
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			carryIn := c.regP & 0x01
			c.setCarry(c.A&0x80 != 0) // Carry from old bit 7
			c.A = (c.A << 1) | carryIn
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2B: // ANC Immediate (illegal opcode)
			c.A &= c.load(c.PC)
			c.setNZ(c.A)
			c.setCarry(c.A&0x80 != 0) // Carry = bit 7 of result (same as N)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2C: // BIT Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x2D: // AND Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x2E: // ROL Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x30: // BMI
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x80 != 0 { // N set: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0x31: // AND (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0x35: // AND Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x36: // ROL Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x38: // SEC
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.regP |= 0x01 // Set Carry flag
			c.TState = 0   // Finished, next cycle is T0 for next opcode
		case 0x39: // AND Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x3D: // AND Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x3E: // ROL Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x40: // RTI
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.TState = 2
		case 0x41: // EOR (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0x45: // EOR Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x46: // LSR Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x48: // PHA
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.TState = 2
		case 0x49: // EOR Immediate
			c.A ^= c.load(c.PC)
			c.setNZ(c.A)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x4A: // LSR Accumulator
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.setCarry(c.A&0x01 != 0) // Carry from old bit 0
			c.A >>= 1
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x4C: // JMP Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x4D: // EOR Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x4E: // LSR Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x50: // BVC
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x40 == 0 { // V clear: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0x51: // EOR (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0x55: // EOR Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x56: // LSR Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x58: // CLI
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.regP &^= 0x04 // Clear Interrupt Disable flag
			c.TState = 0    // Finished, next cycle is T0 for next opcode
		case 0x59: // EOR Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x5D: // EOR Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x5E: // LSR Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x60: // RTS
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.TState = 2
		case 0x61: // ADC (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0x65: // ADC Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x66: // ROR Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x68: // PLA
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.TState = 2
		case 0x69: // ADC Immediate
			c.adc(c.load(c.PC))
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x6A: // ROR Accumulator
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			carryIn := c.regP & 0x01
			c.setCarry(c.A&0x01 != 0) // Carry from old bit 0
			c.A = (c.A >> 1) | (carryIn << 7)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x6C: // JMP Indirect
			c.Operand = uint16(c.load(c.PC)) // Store pointer address low byte
			c.PC++
			c.TState = 2
		case 0x6D: // ADC Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x6E: // ROR Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x70: // BVS
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x40 != 0 { // V set: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0x71: // ADC (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0x75: // ADC Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x76: // ROR Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x78: // SEI
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.regP |= 0x04 // Set Interrupt Disable flag
			c.TState = 0   // Finished, next cycle is T0 for next opcode
		case 0x79: // ADC Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x7D: // ADC Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x7E: // ROR Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x81: // STA (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0x84: // STY Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x85: // STA Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x86: // STX Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0x88: // DEY
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.Y--
			c.setNZ(c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x8A: // TXA
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.A = c.X
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x8C: // STY Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x8D: // STA Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x8E: // STX Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x90: // BCC
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x01 == 0 { // C clear: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0x91: // STA (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0x94: // STY Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x95: // STA Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x96: // STX Zero Page,Y
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0x98: // TYA
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.A = c.Y
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x99: // STA Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0x9A: // TXS
			// Dummy read of the next byte, discarded, PC not advanced. No flags affected.
			c.load(c.PC)
			c.SP = c.X
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x9D: // STA Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xA0: // LDY Immediate
			c.Y = c.load(c.PC)
			c.setNZ(c.Y)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA1: // LDA (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0xA2: // LDX Immediate
			c.X = c.load(c.PC)
			c.setNZ(c.X)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA4: // LDY Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xA6: // LDX Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xA8: // TAY
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.Y = c.A
			c.setNZ(c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xAA: // TAX
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.X = c.A
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xAC: // LDY Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xAD: // LDA Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xAE: // LDX Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xB0: // BCS
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x01 != 0 { // C set: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0xB1: // LDA (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0xB4: // LDY Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xB5: // LDA Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xB6: // LDX Zero Page,Y
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xB8: // CLV
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.regP &^= 0x40 // Clear Overflow flag
			c.TState = 0    // Finished, next cycle is T0 for next opcode
		case 0xB9: // LDA Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xBA: // TSX
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.X = c.SP
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xBC: // LDY Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xBD: // LDA Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xBE: // LDX Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xB3: // LAX (Indirect),Y (illegal opcode)
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0xB7: // LAX Zero Page,Y (illegal opcode)
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xA3: // LAX (Indirect,X) (illegal opcode)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0xA7: // LAX Zero Page (illegal opcode)
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xAF: // LAX Absolute (illegal opcode)
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xBF: // LAX Absolute,Y (illegal opcode)
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xC7: // DCP Zero Page (illegal opcode)
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xC0: // CPY Immediate
			c.compare(c.Y, c.load(c.PC))
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xC1: // CMP (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0xC4: // CPY Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xC5: // CMP Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xC6: // DEC Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xC8: // INY
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.Y++
			c.setNZ(c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xC9: // CMP Immediate
			c.compare(c.A, c.load(c.PC))
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xCA: // DEX
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.X--
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xCC: // CPY Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xCD: // CMP Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xCE: // DEC Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xD0: // BNE
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x02 == 0 { // Z clear: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0xD1: // CMP (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0xD5: // CMP Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xD6: // DEC Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xD8: // CLD
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.regP &^= 0x08 // Clear Decimal mode flag
			c.TState = 0    // Finished, next cycle is T0 for next opcode
		case 0xD9: // CMP Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xDD: // CMP Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xDE: // DEC Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xE0: // CPX Immediate
			c.compare(c.X, c.load(c.PC))
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xE1: // SBC (Indirect,X)
			c.Pointer = c.load(c.PC) // Store ZP pointer base (BAL)
			c.PC++
			c.TState = 2
		case 0xE4: // CPX Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xE5: // SBC Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xE6: // INC Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP address
			c.PC++
			c.TState = 2
		case 0xE8: // INX
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.X++
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xE9: // SBC Immediate
			c.sbc(c.load(c.PC))
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xEC: // CPX Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xED: // SBC Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xEE: // INC Absolute
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xF0: // BEQ
			c.Operand = uint16(c.load(c.PC)) // Store branch offset (reinterpreted as signed later)
			c.PC++
			if c.regP&0x02 != 0 { // Z set: branch taken
				c.TState = 2
			} else {
				c.TState = 0 // Not taken, finished in 2 cycles
			}
		case 0xF1: // SBC (Indirect),Y
			c.Pointer = c.load(c.PC) // Store ZP pointer address (IAL)
			c.PC++
			c.TState = 2
		case 0xF5: // SBC Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xF6: // INC Zero Page,X
			c.Pointer = c.load(c.PC) // Store ZP base address (BAL)
			c.PC++
			c.TState = 2
		case 0xF8: // SED
			// Dummy read of the next byte, discarded, PC not advanced.
			c.load(c.PC)
			c.regP |= 0x08 // Set Decimal mode flag
			c.TState = 0   // Finished, next cycle is T0 for next opcode
		case 0xF9: // SBC Absolute,Y
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xFD: // SBC Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xFE: // INC Absolute,X
			c.Operand = uint16(c.load(c.PC)) // Store address low byte
			c.PC++
			c.TState = 2
		case 0xEA, 0x1A, 0x3A, 0x5A, 0x7A, 0xDA, 0xFA: // NOP (including unofficial 1-byte NOPs)
			// Real 6502 still performs a bus cycle here: it reads the
			// next opcode byte and discards it, without advancing PC.
			c.load(c.PC)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x80, 0x82, 0x89, 0xC2, 0xE2: // NOP Immediate (illegal opcode / DOP)
			c.load(c.PC)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x04, 0x44, 0x64: // NOP Zero Page (illegal opcode / DOP)
			c.Operand = uint16(c.load(c.PC))
			c.PC++
			c.TState = 2
		case 0x14, 0x34, 0x54, 0x74, 0xD4, 0xF4: // NOP Zero Page,X (illegal opcode / DOP)
			c.Pointer = c.load(c.PC)
			c.PC++
			c.TState = 2
		case 0x1C, 0x3C, 0x5C, 0x7C, 0xDC, 0xFC: // NOP Absolute,X (illegal opcode / TOP)
			c.Operand = uint16(c.load(c.PC))
			c.PC++
			c.TState = 2
		case 0xA9: // LDA Immediate
			c.A = c.load(c.PC)
			c.setNZ(c.A)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA5: // LDA Zero Page
			c.Operand = uint16(c.load(c.PC)) // Store ZP offset
			c.PC++
			c.TState = 2 // Move to T2 for reading data from ZP address
		default:
			panic("Unhandled opcode in T1: " + fmt.Sprintf("%02X", c.Opcode))
		}
	case 2:
		// T2: Execute the instruction based on the opcode
		switch c.Opcode {
		case 0x00: // BRK: push PCH
			c.push(c.pch())
			c.TState = 3
		case 0x01: // ORA (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x04, 0x44, 0x64: // NOP Zero Page: dummy read operand
			c.load(c.Operand)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x14, 0x34, 0x54, 0x74, 0xD4, 0xF4: // NOP Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x1C, 0x3C, 0x5C, 0x7C, 0xDC, 0xFC: // NOP Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x05: // ORA Zero Page: read operand, OR with A
			c.A |= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x06: // ASL Zero Page: read old value
			c.Value = c.load(c.Operand)
			c.TState = 3
		case 0x08: // PHP: push status, with B and unused bits set
			c.push(c.regP | 0x30)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0C: // NOP Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x0D: // ORA Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x0E: // ASL Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x10: // BPL: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0x11: // ORA (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0x15: // ORA Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x16: // ASL Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x19: // ORA Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x1D: // ORA Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x1E: // ASL Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			c.PC++
			c.TState = 3
		case 0x20: // JSR: internal dummy read of current stack location
			c.load(0x0100 + uint16(c.SP))
			c.TState = 3
		case 0x21: // AND (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x24: // BIT Zero Page: read operand, update N/V/Z
			c.updateBITFlags(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x25: // AND Zero Page: read operand, AND with A
			c.A &= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x26: // ROL Zero Page: read old value
			c.Value = c.load(c.Operand)
			c.TState = 3
		case 0x28: // PLP: dummy read at current SP, then increment SP
			c.pop()
			c.TState = 3
		case 0x2C: // BIT Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x2D: // AND Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x2E: // ROL Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x30: // BMI: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0x31: // AND (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0x35: // AND Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x36: // ROL Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x39: // AND Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x3D: // AND Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x3E: // ROL Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			c.PC++
			c.TState = 3
		case 0x40: // RTI: dummy read at current SP, then increment SP
			c.pop()
			c.TState = 3
		case 0x41: // EOR (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x45: // EOR Zero Page: read operand, EOR with A
			c.A ^= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x46: // LSR Zero Page: read old value
			c.Value = c.load(c.Operand)
			c.TState = 3
		case 0x48: // PHA: push A
			c.push(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x4C: // JMP Absolute: fetch address high byte, jump
			c.PC = uint16(c.load(c.PC))<<8 | c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x4D: // EOR Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x4E: // LSR Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x50: // BVC: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0x51: // EOR (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0x55: // EOR Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x56: // LSR Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x59: // EOR Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x5D: // EOR Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x5E: // LSR Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			c.PC++
			c.TState = 3
		case 0x60: // RTS: dummy read at current SP, then increment SP
			c.pop()
			c.TState = 3
		case 0x61: // ADC (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x65: // ADC Zero Page: read operand, add with carry
			c.adc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x66: // ROR Zero Page: read old value
			c.Value = c.load(c.Operand)
			c.TState = 3
		case 0x68: // PLA: dummy read at current SP, then increment SP
			c.pop()
			c.TState = 3
		case 0x6C: // JMP Indirect: fetch pointer address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x6D: // ADC Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x6E: // ROR Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x70: // BVS: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0x71: // ADC (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0x75: // ADC Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x76: // ROR Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x79: // ADC Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x7D: // ADC Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0x7E: // ROR Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			c.PC++
			c.TState = 3
		case 0x81: // STA (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x84: // STY Zero Page: write Y
			c.store(c.Operand, c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x85: // STA Zero Page: write A
			c.store(c.Operand, c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x86: // STX Zero Page: write X
			c.store(c.Operand, c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x8C: // STY Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x8D: // STA Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x8E: // STX Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0x90: // BCC: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0x91: // STA (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0x94: // STY Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x95: // STA Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x96: // STX Zero Page,Y: dummy read from BAL before adding Y
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0x99: // STA Absolute,Y: fetch address high byte, add Y (always corrected, no shortcut for stores)
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			c.PC++
			c.TState = 3
		case 0x9D: // STA Absolute,X: fetch address high byte, add X (always corrected, no shortcut for stores)
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			c.PC++
			c.TState = 3
		case 0xA5: // LDA Zero Page
			c.A = c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA7: // LAX Zero Page: read operand into A and X
			c.A = c.load(c.Operand)
			c.X = c.A
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA1: // LDA (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xA3: // LAX (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xA4: // LDY Zero Page: read operand into Y
			c.Y = c.load(c.Operand)
			c.setNZ(c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA6: // LDX Zero Page: read operand into X
			c.X = c.load(c.Operand)
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xAC: // LDY Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xAD: // LDA Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xAF: // LAX Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xAE: // LDX Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xB0: // BCS: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0xB1: // LDA (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0xB3: // LAX (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0xB4: // LDY Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xB5: // LDA Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xB6: // LDX Zero Page,Y: dummy read from BAL before adding Y
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xB7: // LAX Zero Page,Y: dummy read from BAL before adding Y
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xB9: // LDA Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xBF: // LAX Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xBC: // LDY Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xBD: // LDA Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xBE: // LDX Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xC1: // CMP (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xC4: // CPY Zero Page: read operand, compare with Y
			c.compare(c.Y, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xC5: // CMP Zero Page: read operand, compare with A
			c.compare(c.A, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xC6: // DEC Zero Page: read old value
			c.Value = c.load(c.Operand)
			c.TState = 3
		case 0xC7: // DCP Zero Page: read old value
			c.Value = c.load(c.Operand)
			c.TState = 3
		case 0xCC: // CPY Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xCD: // CMP Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xCE: // DEC Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xD0: // BNE: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0xD1: // CMP (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0xD5: // CMP Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xD6: // DEC Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xD9: // CMP Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xDD: // CMP Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xDE: // DEC Absolute,X: fetch address high byte, add X (always corrected, no shortcut for RMW)
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			c.PC++
			c.TState = 3
		case 0xE1: // SBC (Indirect,X): dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xE4: // CPX Zero Page: read operand, compare with X
			c.compare(c.X, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xE5: // SBC Zero Page: read operand, subtract with borrow
			c.sbc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xE6: // INC Zero Page: read old value
			c.Value = c.load(c.Operand)
			c.TState = 3
		case 0xEC: // CPX Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xED: // SBC Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xEE: // INC Absolute: fetch address high byte
			c.Operand |= uint16(c.load(c.PC)) << 8
			c.PC++
			c.TState = 3
		case 0xF0: // BEQ: dummy read, compute new PC if branch taken
			c.load(c.PC)
			target := uint16(int32(c.PC) + int32(int8(c.Operand)))
			if target&0xFF00 == c.PC&0xFF00 {
				c.PC = target
				c.TState = 0 // Finished, no page cross
			} else {
				c.Operand = target // Stash corrected final PC
				c.TState = 3
			}
		case 0xF1: // SBC (Indirect),Y: fetch effective address low byte (BAL)
			c.Operand = uint16(c.load(uint16(c.Pointer)))
			c.TState = 3
		case 0xF5: // SBC Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xF6: // INC Zero Page,X: dummy read from BAL before adding X
			c.load(uint16(c.Pointer))
			c.TState = 3
		case 0xF9: // SBC Absolute,Y: fetch address high byte, add Y
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xFD: // SBC Absolute,X: fetch address high byte, add X
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			} else {
				c.Value = 0
			}
			c.PC++
			c.TState = 3
		case 0xFE: // INC Absolute,X: fetch address high byte, add X (always corrected, no shortcut for RMW)
			low := uint8(c.Operand)
			high := c.load(c.PC)
			sum := uint16(low) + uint16(c.X)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.X) // Corrected address
			c.PC++
			c.TState = 3
		default:
			panic("Unhandled opcode in T2: " + fmt.Sprintf("%02X", c.Opcode))
		}
	case 3:
		// T3: Execute the instruction based on the opcode
		switch c.Opcode {
		case 0x00: // BRK: push PCL
			c.push(c.pcl())
			c.TState = 4
		case 0x01: // ORA (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0x06: // ASL Zero Page: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value <<= 1
			c.TState = 4
		case 0x14, 0x34, 0x54, 0x74, 0xD4, 0xF4: // NOP Zero Page,X: dummy read at BAL+X
			c.load(uint16(c.Pointer + c.X))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x1C, 0x3C, 0x5C, 0x7C, 0xDC, 0xFC: // NOP Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x0C: // NOP Absolute: dummy read operand
			c.load(c.Operand)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0D: // ORA Absolute: read operand, OR with A
			c.A |= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0E: // ASL Absolute: read old value
			c.Value = c.load(c.Operand)
			c.TState = 4
		case 0x10: // BPL: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x30: // BMI: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x50: // BVC: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x70: // BVS: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x11: // ORA (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0x15: // ORA Zero Page,X: read operand, OR with A
			c.A |= c.load(uint16(c.Pointer + c.X)) // zero-page wraparound
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x16: // ASL Zero Page,X: read old value
			addr := uint16(c.Pointer + c.X) // zero-page wraparound
			c.load(addr)
			c.Operand = addr
			c.Value = bus.Data
			c.TState = 4
		case 0x19: // ORA Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A |= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x1D: // ORA Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A |= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x1E: // ASL Absolute,X: dummy read at guess address
			c.load(c.Addr2)
			c.TState = 4
		case 0x20: // JSR: push PCH (of return address - 1)
			c.push(c.pch())
			c.TState = 4
		case 0x21: // AND (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0x26: // ROL Zero Page: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value = (c.Value << 1) | carryIn
			c.TState = 4
		case 0x28: // PLP: pull status from incremented SP
			c.regP = c.load(0x0100 + uint16(c.SP))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2C: // BIT Absolute: read operand, update N/V/Z
			c.updateBITFlags(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2D: // AND Absolute: read operand, AND with A
			c.A &= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2E: // ROL Absolute: read old value
			c.Value = c.load(c.Operand)
			c.TState = 4
		case 0x31: // AND (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0x35: // AND Zero Page,X: read operand, AND with A
			c.A &= c.load(uint16(c.Pointer + c.X)) // zero-page wraparound
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x36: // ROL Zero Page,X: read old value
			addr := uint16(c.Pointer + c.X) // zero-page wraparound
			c.load(addr)
			c.Operand = addr
			c.Value = bus.Data
			c.TState = 4
		case 0x39: // AND Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A &= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x3D: // AND Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A &= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x3E: // ROL Absolute,X: dummy read at guess address
			c.load(c.Addr2)
			c.TState = 4
		case 0x40: // RTI: pull status from incremented SP, then increment SP
			c.regP = c.pop()
			c.TState = 4
		case 0x41: // EOR (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0x46: // LSR Zero Page: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value >>= 1
			c.TState = 4
		case 0x4D: // EOR Absolute: read operand, EOR with A
			c.A ^= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x4E: // LSR Absolute: read old value
			c.Value = c.load(c.Operand)
			c.TState = 4
		case 0x51: // EOR (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0x55: // EOR Zero Page,X: read operand, EOR with A
			c.A ^= c.load(uint16(c.Pointer + c.X)) // zero-page wraparound
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x56: // LSR Zero Page,X: read old value
			addr := uint16(c.Pointer + c.X) // zero-page wraparound
			c.load(addr)
			c.Operand = addr
			c.Value = bus.Data
			c.TState = 4
		case 0x59: // EOR Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A ^= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x5D: // EOR Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A ^= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x5E: // LSR Absolute,X: dummy read at guess address
			c.load(c.Addr2)
			c.TState = 4
		case 0x60: // RTS: pull PCL from incremented SP, then increment SP
			c.Operand = uint16(c.pop()) // Stash PCL until T4
			c.TState = 4
		case 0x61: // ADC (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0x66: // ROR Zero Page: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value = (c.Value >> 1) | (carryIn << 7)
			c.TState = 4
		case 0x6C: // JMP Indirect: fetch target address low byte from pointer
			c.Value = c.load(c.Operand) // Stash target low byte until T4
			c.TState = 4
		case 0x6D: // ADC Absolute: read operand, add with carry
			c.adc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x6E: // ROR Absolute: read old value
			c.Value = c.load(c.Operand)
			c.TState = 4
		case 0x68: // PLA: pull A from incremented SP
			c.A = c.load(0x0100 + uint16(c.SP))
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x71: // ADC (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0x75: // ADC Zero Page,X: read operand, add with carry
			c.adc(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 0                           // Finished, next cycle is T0 for next opcode
		case 0x76: // ROR Zero Page,X: read old value
			addr := uint16(c.Pointer + c.X) // zero-page wraparound
			c.load(addr)
			c.Operand = addr
			c.Value = bus.Data
			c.TState = 4
		case 0x79: // ADC Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.adc(bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x7D: // ADC Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.adc(bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0x7E: // ROR Absolute,X: dummy read at guess address
			c.load(c.Addr2)
			c.TState = 4
		case 0x81: // STA (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0x8C: // STY Absolute: write Y
			c.store(c.Operand, c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x8D: // STA Absolute: write A
			c.store(c.Operand, c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x8E: // STX Absolute: write X
			c.store(c.Operand, c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x90: // BCC: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x91: // STA (Indirect),Y: fetch effective address high byte (BAH), add Y (always corrected)
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF)                  // Guess address, always used for a dummy read
			c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			c.TState = 4
		case 0x94: // STY Zero Page,X: write Y
			c.store(uint16(c.Pointer+c.X), c.Y) // zero-page wraparound
			c.TState = 0                        // Finished, next cycle is T0 for next opcode
		case 0x95: // STA Zero Page,X: write A
			c.store(uint16(c.Pointer+c.X), c.A) // zero-page wraparound
			c.TState = 0                        // Finished, next cycle is T0 for next opcode
		case 0x96: // STX Zero Page,Y: write X
			c.store(uint16(c.Pointer+c.Y), c.X) // zero-page wraparound
			c.TState = 0                        // Finished, next cycle is T0 for next opcode
		case 0x99: // STA Absolute,Y: dummy read at guess address (store always pays this cycle)
			c.load(c.Addr2)
			c.TState = 4
		case 0x9D: // STA Absolute,X: dummy read at guess address (store always pays this cycle)
			c.load(c.Addr2)
			c.TState = 4
		case 0xA1: // LDA (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0xA3: // LAX (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0xAC: // LDY Absolute: read operand into Y
			c.Y = c.load(c.Operand)
			c.setNZ(c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xAD: // LDA Absolute: read operand into A
			c.A = c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xAF: // LAX Absolute: read operand into A and X
			c.A = c.load(c.Operand)
			c.X = c.A
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xAE: // LDX Absolute: read operand into X
			c.X = c.load(c.Operand)
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB0: // BCS: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB1: // LDA (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0xB3: // LAX (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0xB4: // LDY Zero Page,X: read operand into Y
			c.Y = c.load(uint16(c.Pointer + c.X)) // zero-page wraparound
			c.setNZ(c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB5: // LDA Zero Page,X: read operand into A
			c.A = c.load(uint16(c.Pointer + c.X)) // zero-page wraparound
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB6: // LDX Zero Page,Y: read operand into X
			c.X = c.load(uint16(c.Pointer + c.Y)) // zero-page wraparound
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB7: // LAX Zero Page,Y: read operand into A and X
			c.A = c.load(uint16(c.Pointer + c.Y)) // zero-page wraparound
			c.X = c.A
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB9: // LDA Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A = bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xBF: // LAX Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A = bus.Data
				c.X = c.A
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xBC: // LDY Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.Y = bus.Data
				c.setNZ(c.Y)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xBD: // LDA Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A = bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xBE: // LDX Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.X = bus.Data
				c.setNZ(c.X)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xC1: // CMP (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0xC6: // DEC Zero Page: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value--
			c.TState = 4
		case 0xC7: // DCP Zero Page: dummy write-back of old value, compute new value and compare
			c.store(c.Operand, c.Value)
			c.Value--
			c.compare(c.A, c.Value)
			c.TState = 4
		case 0xCC: // CPY Absolute: read operand, compare with Y
			c.compare(c.Y, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xCD: // CMP Absolute: read operand, compare with A
			c.compare(c.A, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xCE: // DEC Absolute: read old value
			c.Value = c.load(c.Operand)
			c.TState = 4
		case 0xD0: // BNE: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xD1: // CMP (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0xD5: // CMP Zero Page,X: read operand, compare with A
			c.compare(c.A, c.load(uint16(c.Pointer+c.X))) // zero-page wraparound
			c.TState = 0                                  // Finished, next cycle is T0 for next opcode
		case 0xD6: // DEC Zero Page,X: read old value
			addr := uint16(c.Pointer + c.X) // zero-page wraparound
			c.Value = c.load(addr)
			c.Operand = addr
			c.TState = 4
		case 0xD9: // CMP Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.compare(c.A, bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xDD: // CMP Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.compare(c.A, bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xDE: // DEC Absolute,X: dummy read at guess address
			c.load(c.Addr2)
			c.TState = 4
		case 0xE1: // SBC (Indirect,X): fetch effective address low byte
			c.Operand = uint16(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 4
		case 0xE6: // INC Zero Page: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value++
			c.TState = 4
		case 0xEC: // CPX Absolute: read operand, compare with X
			c.compare(c.X, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xED: // SBC Absolute: read operand, subtract with borrow
			c.sbc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xEE: // INC Absolute: read old value
			c.Value = c.load(c.Operand)
			c.TState = 4
		case 0xF0: // BEQ: dummy read at wrong address, fix PCH
			c.load((c.PC & 0xFF00) | (c.Operand & 0x00FF)) // Old PCH, new PCL
			c.PC = c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xF1: // SBC (Indirect),Y: fetch effective address high byte (BAH), add Y
			low := uint8(c.Operand)
			high := c.load(uint16(c.Pointer + 1)) // zero-page wraparound
			sum := uint16(low) + uint16(c.Y)
			c.Addr2 = uint16(high)<<8 | (sum & 0xFF) // Guess address (may have wrong high byte)
			if sum > 0xFF {
				c.Value = 1                                               // Page crossed
				c.Operand = (uint16(high)<<8 | uint16(low)) + uint16(c.Y) // Corrected address
			} else {
				c.Value = 0
			}
			c.TState = 4
		case 0xF5: // SBC Zero Page,X: read operand, subtract with borrow
			c.sbc(c.load(uint16(c.Pointer + c.X))) // zero-page wraparound
			c.TState = 0                           // Finished, next cycle is T0 for next opcode
		case 0xF6: // INC Zero Page,X: read old value
			addr := uint16(c.Pointer + c.X) // zero-page wraparound
			c.Value = c.load(addr)
			c.Operand = addr
			c.TState = 4
		case 0xF9: // SBC Absolute,Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.sbc(bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xFD: // SBC Absolute,X: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.sbc(bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 4 // Page crossed: need corrected re-read
			}
		case 0xFE: // INC Absolute,X: dummy read at guess address
			c.load(c.Addr2)
			c.TState = 4
		default:
			panic("Unhandled opcode in T3: " + fmt.Sprintf("%02X", c.Opcode))
		}
	case 4:
		// T4: Execute the instruction based on the opcode
		switch c.Opcode {
		case 0x00: // BRK/IRQ/NMI: push Status. Only a real BRK sets the B flag.
			//
			// The B flag has no real physical storage in the 6502 - it only
			// exists transiently in the byte pushed to the stack. regP must
			// not be trusted to hold a correct B bit here: PLP/RTI copy the
			// full pulled byte (including whatever B happened to be) back
			// into regP, so a stale 1 can persist indefinitely. Force it to
			// 0 first, then set it only for a genuine BRK.
			status := (c.regP &^ 0x10) | 0x20
			if c.Interrupt == 0 {
				status |= 0x10
			}
			c.push(status)
			c.TState = 5
		case 0x01: // ORA (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0xC1: // CMP (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0xC6: // DEC Zero Page: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xC7: // DCP Zero Page: write new value (already compared in T3)
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xCE: // DEC Absolute: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value--
			c.TState = 5
		case 0x06: // ASL Zero Page: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0E: // ASL Absolute: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value <<= 1
			c.TState = 5
		case 0x11: // ORA (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A |= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0x16: // ASL Zero Page,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value <<= 1
			c.TState = 5
		case 0x19: // ORA Absolute,Y: re-read at corrected address
			c.A |= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x1D: // ORA Absolute,X: re-read at corrected address
			c.A |= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x1E: // ASL Absolute,X: read old value at corrected address
			c.Value = c.load(c.Operand)
			c.TState = 5
		case 0x20: // JSR: push PCL (of return address - 1)
			c.push(c.pcl())
			c.TState = 5
		case 0x21: // AND (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0x26: // ROL Zero Page: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2E: // ROL Absolute: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value = (c.Value << 1) | carryIn
			c.TState = 5
		case 0x31: // AND (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A &= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0x36: // ROL Zero Page,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value = (c.Value << 1) | carryIn
			c.TState = 5
		case 0x39: // AND Absolute,Y: re-read at corrected address
			c.A &= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x3D: // AND Absolute,X: re-read at corrected address
			c.A &= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x3E: // ROL Absolute,X: read old value at corrected address
			c.Value = c.load(c.Operand)
			c.TState = 5
		case 0x40: // RTI: pull PCL from incremented SP, then increment SP
			c.Operand = uint16(c.pop()) // Stash PCL until T5
			c.TState = 5
		case 0x41: // EOR (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0x46: // LSR Zero Page: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x4E: // LSR Absolute: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value >>= 1
			c.TState = 5
		case 0x51: // EOR (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A ^= bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0x56: // LSR Zero Page,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value >>= 1
			c.TState = 5
		case 0x59: // EOR Absolute,Y: re-read at corrected address
			c.A ^= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x5D: // EOR Absolute,X: re-read at corrected address
			c.A ^= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x5E: // LSR Absolute,X: read old value at corrected address
			c.Value = c.load(c.Operand)
			c.TState = 5
		case 0x60: // RTS: pull PCH from incremented SP
			c.PC = uint16(c.load(0x0100+uint16(c.SP)))<<8 | c.Operand
			c.TState = 5
		case 0x61: // ADC (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0x66: // ROR Zero Page: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x6C: // JMP Indirect: fetch target address high byte, jump
			c.PC = uint16(c.load((c.Operand&0xFF00)|((c.Operand+1)&0x00FF)))<<8 | uint16(c.Value) // Wraps within page (hardware bug)
			c.TState = 0                                                                          // Finished, next cycle is T0 for next opcode
		case 0x6E: // ROR Absolute: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value = (c.Value >> 1) | (carryIn << 7)
			c.TState = 5
		case 0x71: // ADC (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.adc(bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0x76: // ROR Zero Page,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value = (c.Value >> 1) | (carryIn << 7)
			c.TState = 5
		case 0x79: // ADC Absolute,Y: re-read at corrected address
			c.adc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x7D: // ADC Absolute,X: re-read at corrected address
			c.adc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x7E: // ROR Absolute,X: read old value at corrected address
			c.Value = c.load(c.Operand)
			c.TState = 5
		case 0x81: // STA (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0x91: // STA (Indirect),Y: dummy read at guess address (store always pays this cycle)
			c.load(c.Addr2)
			c.TState = 5
		case 0x99: // STA Absolute,Y: write A at corrected address
			c.store(c.Operand, c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x9D: // STA Absolute,X: write A at corrected address
			c.store(c.Operand, c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA1: // LDA (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0xA3: // LAX (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0xB1: // LDA (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A = bus.Data
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0xB3: // LAX (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.A = bus.Data
				c.X = c.A // LAX loads both A and X
				c.setNZ(c.A)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0xB9: // LDA Absolute,Y: re-read at corrected address
			c.A = c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xBF: // LAX Absolute,Y: re-read at corrected address
			c.A = c.load(c.Operand)
			c.X = c.A
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xBC: // LDY Absolute,X: re-read at corrected address
			c.Y = c.load(c.Operand)
			c.setNZ(c.Y)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xBD: // LDA Absolute,X: re-read at corrected address
			c.A = c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xBE: // LDX Absolute,Y: re-read at corrected address
			c.X = c.load(c.Operand)
			c.setNZ(c.X)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xD1: // CMP (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.compare(c.A, bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0xD6: // DEC Zero Page,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value--
			c.TState = 5
		case 0xD9: // CMP Absolute,Y: re-read at corrected address
			c.compare(c.A, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xDD: // CMP Absolute,X: re-read at corrected address
			c.compare(c.A, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xDE: // DEC Absolute,X: read old value at corrected address
			c.Value = c.load(c.Operand)
			c.TState = 5
		case 0xE1: // SBC (Indirect,X): fetch effective address high byte
			c.Operand |= uint16(c.load(uint16(c.Pointer+c.X+1))) << 8 // zero-page wraparound
			c.TState = 5
		case 0xE6: // INC Zero Page: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xEE: // INC Absolute: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value++
			c.TState = 5
		case 0xF1: // SBC (Indirect),Y: read at guess address, finish unless page crossed
			c.load(c.Addr2)
			if c.Value == 0 {
				c.sbc(bus.Data)
				c.TState = 0 // Finished, next cycle is T0 for next opcode
			} else {
				c.TState = 5 // Page crossed: need corrected re-read
			}
		case 0xF6: // INC Zero Page,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value++
			c.TState = 5
		case 0xF9: // SBC Absolute,Y: re-read at corrected address
			c.sbc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xFD: // SBC Absolute,X: re-read at corrected address
			c.sbc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x1C, 0x3C, 0x5C, 0x7C, 0xDC, 0xFC: // NOP Absolute,X: dummy re-read at corrected address
			c.load(c.Operand)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xFE: // INC Absolute,X: read old value at corrected address
			c.Value = c.load(c.Operand)
			c.TState = 5
		default:
			panic("Unhandled opcode in T4: " + fmt.Sprintf("%02X", c.Opcode))
		}
	case 5:
		// T5: Execute the instruction based on the opcode
		switch c.Opcode {
		case 0x7E: // ROR Absolute,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value = (c.Value >> 1) | (carryIn << 7)
			c.TState = 6
		case 0x00: // BRK/IRQ: fetch new PCL from vector ($FFFA for NMI)
			vec := uint16(0xFFFE)
			if c.Interrupt == 2 {
				vec = 0xFFFA
			}
			c.Operand = uint16(c.load(vec)) // Stash new PCL until T6
			c.regP |= P_INTERRUPT
			c.TState = 6
		case 0x81: // STA (Indirect,X): write A
			c.store(c.Operand, c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x01: // ORA (Indirect,X): read operand, OR with A
			c.A |= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x0E: // ASL Absolute: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x11: // ORA (Indirect),Y: re-read at corrected address
			c.A |= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x16: // ASL Zero Page,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x1E: // ASL Absolute,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value <<= 1
			c.TState = 6
		case 0x36: // ROL Zero Page,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x56: // LSR Zero Page,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x20: // JSR: fetch target address high byte, jump
			c.PC = uint16(c.load(c.PC))<<8 | c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x21: // AND (Indirect,X): read operand, AND with A
			c.A &= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x31: // AND (Indirect),Y: re-read at corrected address
			c.A &= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x2E: // ROL Absolute: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x3E: // ROL Absolute,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			carryIn := c.regP & 0x01
			c.setCarry(c.Value&0x80 != 0) // Carry from old bit 7
			c.Value = (c.Value << 1) | carryIn
			c.TState = 6
		case 0x40: // RTI: pull PCH from incremented SP, jump
			c.PC = uint16(c.load(0x0100+uint16(c.SP)))<<8 | c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x41: // EOR (Indirect,X): read operand, EOR with A
			c.A ^= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x51: // EOR (Indirect),Y: re-read at corrected address
			c.A ^= c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x4E: // LSR Absolute: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x5E: // LSR Absolute,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.setCarry(c.Value&0x01 != 0) // Carry from old bit 0
			c.Value >>= 1
			c.TState = 6
		case 0x60: // RTS: dummy read at popped PC, then increment PC
			c.load(c.PC)
			c.PC++
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x61: // ADC (Indirect,X): read operand, add with carry
			c.adc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x6E: // ROR Absolute: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x76: // ROR Zero Page,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x91: // STA (Indirect),Y: write A at corrected address
			c.store(c.Operand, c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x71: // ADC (Indirect),Y: re-read at corrected address
			c.adc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA1: // LDA (Indirect,X): read operand into A
			c.A = c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xA3: // LAX (Indirect,X): read operand into A and X
			c.A = c.load(c.Operand)
			c.X = c.A
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB1: // LDA (Indirect),Y: re-read at corrected address
			c.A = c.load(c.Operand)
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xB3: // LAX (Indirect),Y: re-read at corrected address
			c.A = c.load(c.Operand)
			c.X = c.A // LAX loads both A and X
			c.setNZ(c.A)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xC1: // CMP (Indirect,X): read operand, compare with A
			c.compare(c.A, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xD1: // CMP (Indirect),Y: re-read at corrected address
			c.compare(c.A, c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xCE: // DEC Absolute: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xD6: // DEC Zero Page,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xDE: // DEC Absolute,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value--
			c.TState = 6
		case 0xE1: // SBC (Indirect,X): read operand, subtract with borrow
			c.sbc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xEE: // INC Absolute: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xF1: // SBC (Indirect),Y: re-read at corrected address
			c.sbc(c.load(c.Operand))
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xF6: // INC Zero Page,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xFE: // INC Absolute,X: dummy write-back of old value, compute new value
			c.store(c.Operand, c.Value)
			c.Value++
			c.TState = 6
		default:
			panic("Unhandled opcode in T5: " + fmt.Sprintf("%02X", c.Opcode))
		}
	case 6:
		// T6: Execute the instruction based on the opcode
		switch c.Opcode {
		case 0x00: // BRK/IRQ: fetch new PCH from vector ($FFFB for NMI), jump
			vec := uint16(0xFFFF)
			if c.Interrupt == 2 {
				vec = 0xFFFB
			}
			c.PC = uint16(c.load(vec))<<8 | c.Operand
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x1E: // ASL Absolute,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x7E: // ROR Absolute,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x5E: // LSR Absolute,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0x3E: // ROL Absolute,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xDE: // DEC Absolute,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		case 0xFE: // INC Absolute,X: write new value
			c.store(c.Operand, c.Value)
			c.setNZ(c.Value)
			c.TState = 0 // Finished, next cycle is T0 for next opcode
		default:
			panic("Unhandled opcode in T6: " + fmt.Sprintf("%02X", c.Opcode))
		}
	default:
		panic("Invalid T-state: " + fmt.Sprintf("%d", c.TState))
	}
	c.irq.clock(cia1.IRQ || vic.IRQ, i, irqPoll(opcode, tstate, c.TState), false)
}

// pch returns the high byte of PC
func (c *CPU) pch() uint8 {
	return uint8(c.PC >> 8)
}

// pcl returns the lower byte of PC
func (c *CPU) pcl() uint8 {
	return uint8(c.PC & 0xFF)
}

func (c *CPU) setNZ(val uint8) {
	c.setZero(val)
	c.setSign(val&P_SIGN != 0)
}

func (c *CPU) setZero(val uint8) {
	if val == 0 {
		c.regP |= P_ZERO // Set Zero flag
	} else {
		c.regP &^= P_ZERO // Clear Zero flag
	}
}

func (c *CPU) setSign(cond bool) {
	if cond {
		c.regP |= P_SIGN // Set Negative flag
	} else {
		c.regP &^= P_SIGN // Clear Negative flag
	}
}

// setCarry sets or clears the Carry flag based on cond.
func (c *CPU) setCarry(cond bool) {
	if cond {
		c.regP |= 0x01
	} else {
		c.regP &^= 0x01
	}
}

// updateBITFlags sets the Zero flag from A&M, and copies bits 7 and 6 of M
// directly into the Negative and Overflow flags. Used only by BIT.
func (c *CPU) updateBITFlags(m uint8) {
	if c.A&m == 0 {
		c.regP |= 0x02 // Set Zero flag
	} else {
		c.regP &^= 0x02 // Clear Zero flag
	}
	if m&0x80 != 0 {
		c.regP |= 0x80 // Set Negative flag
	} else {
		c.regP &^= 0x80 // Clear Negative flag
	}
	if m&0x40 != 0 {
		c.regP |= 0x40 // Set Overflow flag
	} else {
		c.regP &^= 0x40 // Clear Overflow flag
	}
}

// adc adds value and the Carry flag to A (binary mode only; BCD/decimal mode
// is not yet implemented), updating Carry, Overflow, Negative and Zero flags.
func (c *CPU) adc(value uint8) {
	if c.regP&P_DECIMAL != 0 {
		c.DecimalADCCount++
		c.LastDecimalADCPC = c.PC
	}
	carryIn := uint16(c.regP & 0x01)
	sum := uint16(c.A) + uint16(value) + carryIn
	result := uint8(sum)
	c.setCarry(sum > 0xFF)
	if (c.A^value)&0x80 == 0 && (c.A^result)&0x80 != 0 {
		c.regP |= 0x40 // Set Overflow flag
	} else {
		c.regP &^= 0x40
	}
	c.A = result
	c.setNZ(c.A)
}

// compare subtracts value from reg (without storing the result) and updates
// Carry, Negative and Zero flags. Used by CMP, CPX and CPY.
func (c *CPU) compare(reg, value uint8) {
	result := reg - value
	c.setCarry(reg >= value)
	c.setNZ(result)
}

// sbc subtracts value and the borrow (inverse Carry) from A. On the 6502,
// SBC(M) is equivalent to ADC(^M) since A - M - (1-C) == A + ^M + C.
func (c *CPU) sbc(value uint8) {
	c.adc(^value)
}

// cpuWriteCycles records, per opcode, which T-states drive a write cycle
// onto the bus: bit T is set if T-state T writes. It lets TickPhi2 answer
// "is the cycle I am about to run a read?" without executing it, which is
// what the 6510's RDY/BA handling needs (see TickPhi2).
//
// T-state 0 is always an opcode fetch and so never writes, which is what
// makes indexing by c.Opcode safe: at T0 the field still holds the
// *previous* instruction's opcode, but every entry has bit 0 clear.
// Hardware interrupts run BRK's microcode with c.Opcode set to $00, so the
// $00 entry covers the IRQ/NMI push sequence too.
//
// Opcodes the core does not implement read as 0, which degrades to the
// conservative "always stall" behaviour; they panic on execution anyway.
// TestCPUWriteCyclesTableMatchesMicrocode re-derives this table from the
// microcode and fails if the two ever drift apart.
var cpuWriteCycles = [256]uint16{
	0x001C, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0018, 0x0000, 0x0004, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, // $00
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0060, 0x0000, // $10
	0x0018, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0018, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, // $20
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0060, 0x0000, // $30
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0018, 0x0000, 0x0004, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, // $40
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0060, 0x0000, // $50
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0018, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, // $60
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0060, 0x0000, // $70
	0x0000, 0x0020, 0x0000, 0x0000, 0x0004, 0x0004, 0x0004, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0008, 0x0008, 0x0008, 0x0000, // $80
	0x0000, 0x0020, 0x0000, 0x0000, 0x0008, 0x0008, 0x0008, 0x0000, 0x0000, 0x0010, 0x0000, 0x0000, 0x0000, 0x0010, 0x0000, 0x0000, // $90
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, // $A0
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, // $B0
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0018, 0x0018, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, // $C0
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0060, 0x0000, // $D0
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0018, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, // $E0
	0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0030, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0000, 0x0060, 0x0000, // $F0
}

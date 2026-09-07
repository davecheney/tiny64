package tiny64

// VIA emulates a MOS 6522 Versatile Interface Adapter. The 1541 has two:
// VIA1 (UC1) talks to the C64 over the IEC serial bus and reads the drive's
// device-address jumpers; VIA2 (UC3) drives the stepper motor, read/write
// head and write-protect sensor. Only the generic register-level behavior
// (ports, timers, interrupts) is modeled here - VIA2's disk-specific wiring
// is stubbed for now, since GCR/track emulation is out of scope.
type VIA struct {
	ORA, ORB   uint8 // Output Register A/B
	DDRA, DDRB uint8 // Data Direction Register A/B (1 = output)

	t1c, t1l uint16 // Timer 1 counter/latch
	t2c, t2l uint16 // Timer 2 counter/latch

	acr uint8 // Auxiliary Control Register (bit6: Timer 1 continuous vs one-shot)
	pcr uint8 // Peripheral Control Register (CA1/CA2/CB1/CB2 modes, not modeled)
	ifr uint8 // Interrupt Flag Register
	ier uint8 // Interrupt Enable Register

	// IRQ is the chip's interrupt output line: asserted whenever any
	// enabled interrupt flag is set (IFR & IER & 0x7F != 0).
	IRQ bool
}

var via1, via2 VIA

// Via1 returns the singleton VIA1 (IEC serial bus, device address
// jumpers), for debugging/tracing tools.
func Via1() *VIA { return &via1 }

// Via2 returns the singleton VIA2 (stepper motor/head/write-protect,
// stubbed), for debugging/tracing tools.
func Via2() *VIA { return &via2 }

// effective returns the electrical state of a VIA port: output-configured
// bits (ddr=1) reflect the written value, input-configured bits float high
// since no external device is modeled on that line yet.
func viaEffective(data, ddr uint8) uint8 {
	return (data & ddr) | ^ddr
}

// Load reads a VIA register, mirrored every 16 addresses ($n0-$nF).
func (v *VIA) Load(addr uint16) uint8 {
	switch addr & 0xF {
	case 0x0:
		return viaEffective(v.ORB, v.DDRB)
	case 0x1:
		return viaEffective(v.ORA, v.DDRA)
	case 0x2:
		return v.DDRB
	case 0x3:
		return v.DDRA
	case 0x4:
		v.ifr &^= 0x40 // reading T1C-L clears the Timer 1 interrupt flag
		v.updateIRQ()
		return uint8(v.t1c)
	case 0x5:
		return uint8(v.t1c >> 8)
	case 0x6:
		return uint8(v.t1l)
	case 0x7:
		return uint8(v.t1l >> 8)
	case 0x8:
		v.ifr &^= 0x20 // reading T2C-L clears the Timer 2 interrupt flag
		v.updateIRQ()
		return uint8(v.t2c)
	case 0x9:
		return uint8(v.t2c >> 8)
	case 0xB:
		return v.acr
	case 0xC:
		return v.pcr
	case 0xD:
		flags := v.ifr
		if v.ifr&v.ier&0x7F != 0 {
			flags |= 0x80
		}
		return flags
	case 0xE:
		return v.ier | 0x80
	case 0xF: // ORA, no handshake
		return viaEffective(v.ORA, v.DDRA)
	default:
		return 0
	}
}

// Store writes a VIA register, mirrored every 16 addresses ($n0-$nF).
func (v *VIA) Store(addr uint16, val uint8) {
	switch addr & 0xF {
	case 0x0:
		v.ORB = val
	case 0x1:
		v.ORA = val
	case 0x2:
		v.DDRB = val
	case 0x3:
		v.DDRA = val
	case 0x4:
		v.t1l = (v.t1l & 0xFF00) | uint16(val)
	case 0x5:
		v.t1l = (v.t1l & 0x00FF) | uint16(val)<<8
		v.t1c = v.t1l
		v.ifr &^= 0x40
	case 0x6:
		v.t1l = (v.t1l & 0xFF00) | uint16(val)
	case 0x7:
		v.t1l = (v.t1l & 0x00FF) | uint16(val)<<8
		v.ifr &^= 0x40
	case 0x8:
		v.t2l = (v.t2l & 0xFF00) | uint16(val)
	case 0x9:
		v.t2l = (v.t2l & 0x00FF) | uint16(val)<<8
		v.t2c = v.t2l
		v.ifr &^= 0x20
	case 0xB:
		v.acr = val
	case 0xC:
		v.pcr = val
	case 0xD:
		v.ifr &^= val & 0x7F // writing a 1 bit clears that flag
	case 0xE:
		if val&0x80 != 0 {
			v.ier |= val & 0x7F
		} else {
			v.ier &^= val & 0x7F
		}
	case 0xF:
		v.ORA = val
	}
	v.updateIRQ()
}

// updateIRQ recomputes the IRQ output line from the current flag/enable
// registers.
func (v *VIA) updateIRQ() {
	v.IRQ = v.ifr&v.ier&0x7F != 0
}

// Tick advances both timers by one Phi2 cycle. Timer 1 reloads from its
// latch on underflow when ACR bit 6 selects continuous mode; Timer 2 only
// ever counts once (one-shot) until rewritten, matching real 6522 timer
// modes closely enough for DOS ROM delay loops.
func (v *VIA) Tick() {
	if v.t1c > 0 {
		v.t1c--
	}
	if v.t1c == 0 {
		v.ifr |= 0x40
		if v.acr&0x40 != 0 {
			v.t1c = v.t1l
		}
	}

	if v.t2c > 0 {
		v.t2c--
	}
	if v.t2c == 0 {
		v.ifr |= 0x20
	}

	v.updateIRQ()
}

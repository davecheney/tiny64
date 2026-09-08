package tiny64

import "strings"

// The IEC serial bus is open-collector: each line is asserted
// (electrically low) if ANY device pulls it low, and only released (high,
// via pull-up) when every device releases it. Only one drive is modeled,
// so the two drivers are the C64's CIA2 and the drive's own VIA1.
//
// CIA2 Port A ($DD00) bits 3/4/5 (ATN/CLOCK/DATA OUT) drive the bus
// through inverting 7406 buffers, so a bit WRITTEN as 1 pulls its line low
// - asserted. That is the convention the KERNAL uses throughout: CLKLO is
// LDA $DD00 / ORA #$10 / STA $DD00, and CLKHI is the matching AND #$EF.
// There is no separate test-harness state: cmd/drivec (or a real CIA2)
// both just manipulate CIA2's registers directly via CIA2().

// iecPeripheral is a device hanging off the IEC bus. Because the bus is
// open-collector, a peripheral only ever says whether it is *pulling* a
// line low; releasing is the absence of that. iecTick advances whatever
// internal clocking the peripheral needs, once per system Phi2 cycle.
//
// The interface exists so the bus can carry and address devices without
// knowing what they are. How faithfully a device is modelled is its own
// business: everything the bus needs is the three lines and an address.
type iecPeripheral interface {
	iecCLKOut() bool
	iecDATAOut() bool
	iecTick()

	// iecAddress is the primary address the device answers to, 8-11 for
	// drives. Nothing on the wire carries it - the device compares it
	// against the LISTEN/TALK byte itself - but the bus needs it to tell
	// two devices apart, since an address is the only thing that makes a
	// device distinct as far as the protocol is concerned.
	iecAddress() uint8
}

// iecBus holds the peripherals currently attached to the bus. The C64 is
// not in here: it drives the bus through CIA2, which is always present.
var iecBus []iecPeripheral

// attachIEC adds a peripheral to the bus, replacing whatever was already
// answering to its address.
//
// Address, not type, is the thing that has to be unique. Two devices
// jumpered to the same address is a wiring mistake that a real bus punishes
// rather than diagnoses: both see the LISTEN, both answer, and their
// open-collector drivers hold the lines low against each other so the
// computer sees a garbled byte or nothing at all. That is faithful, but it
// is not useful, and the failure it produces here - a directory that loads
// as ?FILE NOT FOUND - looks nothing like its cause. So the last device
// plugged in at an address wins, and the previous occupant comes off.
func attachIEC(p iecPeripheral) {
	// Both paths below build a new slice rather than writing into the
	// existing one. Callers snapshot iecBus to restore it later, and any
	// write into the shared backing array reaches those snapshots: the
	// replace path by assigning through it directly, the append path by
	// filling spare capacity. Neither changes the snapshot's length, so a
	// corrupted one still looks entirely well formed. See detachIEC.
	for i, existing := range iecBus {
		if existing.iecAddress() == p.iecAddress() {
			replaced := append([]iecPeripheral(nil), iecBus...)
			replaced[i] = p
			iecBus = replaced
			return
		}
	}
	iecBus = append(append(make([]iecPeripheral, 0, len(iecBus)+1), iecBus...), p)
}

// detachIEC removes whatever is answering to p's address.
//
// This builds a new slice rather than filtering into iecBus[:0]. The
// in-place idiom is the usual one, but it is wrong for a package-level
// variable that callers hold references to: the filter shortens the slice
// without giving up the array, so the detached device's slot is still
// shared, and the next attach reuses it. Anything still looking at the bus
// then acquires a device it never saw attached. That is not a leak that
// shows up as a leak - the length stays right, only the contents are wrong -
// and it is what made a drive appear to survive a test that detached it.
func detachIEC(p iecPeripheral) {
	kept := make([]iecPeripheral, 0, len(iecBus))
	for _, existing := range iecBus {
		if existing.iecAddress() != p.iecAddress() {
			kept = append(kept, existing)
		}
	}
	iecBus = kept
}

// iecTick advances every attached peripheral by one Phi2 cycle.
func iecTick() {
	for _, p := range iecBus {
		p.iecTick()
	}
}

// ATNAsserted, CLKAsserted and DATAAsserted report the actual IEC bus line
// states, combining CIA2's output with every attached peripheral's. Only
// the C64 drives ATN.
func ATNAsserted() bool {
	return cia2AtnOut()
}

func CLKAsserted() bool {
	if cia2ClkOut() {
		return true
	}
	for _, p := range iecBus {
		if p.iecCLKOut() {
			return true
		}
	}
	return false
}

func DATAAsserted() bool {
	if cia2DataOut() {
		return true
	}
	for _, p := range iecBus {
		if p.iecDATAOut() {
			return true
		}
	}
	return false
}

// cia2AtnOut/cia2ClkOut/cia2DataOut report whether CIA2 is currently
// pulling the corresponding line low: the bit must be configured as an
// output (DDRA=1) and written as 1, which the inverting buffer turns into
// a low on the bus.
func cia2AtnOut() bool {
	return cia2.DDRA&0x08 != 0 && cia2.PRA&0x08 != 0
}

func cia2ClkOut() bool {
	return cia2.DDRA&0x10 != 0 && cia2.PRA&0x10 != 0
}

func cia2DataOut() bool {
	return cia2.DDRA&0x20 != 0 && cia2.PRA&0x20 != 0
}

// SetCIA2ATN, SetCIA2CLK and SetCIA2DATA drive CIA2's Port A as if a C64
// (or a test harness standing in for one) wanted to assert/release the
// corresponding IEC line: they configure the bit as an output and write
// it the way the KERNAL does, for tools like cmd/drivec.
func SetCIA2ATN(asserted bool)  { setCIA2OutputBit(0x08, asserted) }
func SetCIA2CLK(asserted bool)  { setCIA2OutputBit(0x10, asserted) }
func SetCIA2DATA(asserted bool) { setCIA2OutputBit(0x20, asserted) }

func setCIA2OutputBit(bit uint8, asserted bool) {
	cia2.DDRA |= bit
	if asserted {
		cia2.PRA |= bit
	} else {
		cia2.PRA &^= bit
	}
}

// cia2ReadPRA constructs CIA2 Port A's read value. Bits 7/6 (DATA/CLOCK
// IN) are inputs fed from the bus through inverting buffers, so they read
// the live wired-AND state of the whole bus using "0 = asserted" - which
// is how a device notices another device holding a line down. Every other
// bit, including 5/4/3 (DATA/CLOCK/ATN OUT), is an ordinary port bit and
// reads back what was written to it, since that's what the KERNAL's
// read-modify-write sequences (LDA $DD00 / ORA #$10 / STA $DD00) rely on
// to change one line without disturbing the others.
func cia2ReadPRA() uint8 {
	base := effective(cia2.PRA, cia2.DDRA) & 0x3F

	var in uint8 = 0xC0 // bits 7,6 default released (1) unless asserted
	if DATAAsserted() {
		in &^= 0x80
	}
	if CLKAsserted() {
		in &^= 0x40
	}

	return base | in
}

// via1ClkOut reports whether VIA1 is currently driving CLOCK OUT (PRB bit
// 3) low; it only has effect when that bit is configured as an output.
func via1ClkOut() bool {
	return via1.DDRB&0x08 != 0 && via1.ORB&0x08 != 0
}

// via1DataOut reports whether VIA1 is currently pulling DATA low: either
// it has explicitly driven DATA OUT (PRB bit 1), or - the classic IEC
// auto-acknowledge quirk - ATN is asserted on the bus and the drive hasn't
// yet set its ATN acknowledge bit (PRB bit 4) to override that.
func via1DataOut() bool {
	dataOut := via1.DDRB&0x02 != 0 && via1.ORB&0x02 != 0
	atnAck := via1.DDRB&0x10 != 0 && via1.ORB&0x10 != 0
	return dataOut || (ATNAsserted() && !atnAck)
}

// via1ReadPRB constructs VIA1's Port B read value: ATN IN/device jumpers/
// CLOCK IN/DATA IN (bits 7,6,5,2,0) always reflect the live bus/jumper
// state regardless of DDR, while ATN ACK/CLOCK OUT/DATA OUT (bits 4,3,1)
// use normal DDR-effective read-back.
func via1ReadPRB() uint8 {
	var in uint8
	if ATNAsserted() {
		in |= 0x80
	}
	// Device address jumpers: 00 = device #8 (both bits 0).
	if CLKAsserted() {
		in |= 0x04
	}
	if DATAAsserted() {
		in |= 0x01
	}

	out := viaEffective(via1.ORB, via1.DDRB) & 0x1A
	return in | out
}

// via1AtnAck reports whether VIA1 has set its ATN acknowledge bit (PRB bit
// 4), overriding the automatic ATN->DATA pulldown.
func via1AtnAck() bool {
	return via1.DDRB&0x10 != 0 && via1.ORB&0x10 != 0
}

// via1SampleATN presents the current bus ATN state to VIA1's CA1 pin,
// which is how the drive learns that the C64 wants its attention: the
// 7406 inverter between the bus and the chip means an asserted (low) ATN
// arrives at CA1 as a high level, and the DOS ROM programs PCR bit 0 for
// a low-to-high active edge and enables the CA1 interrupt, so asserting
// ATN interrupts the drive into its command handler.
func via1SampleATN() {
	via1.setCA1(ATNAsserted())
}

// IECStatus returns a human-readable summary of the IEC bus lines, who is
// driving each one, and the attached peripherals - for debugging tools.
func IECStatus() string {
	line := func(name string, asserted bool, drivers ...string) string {
		state := "released"
		if asserted {
			state = "asserted"
		}
		if len(drivers) == 0 {
			return name + ": " + state
		}
		return name + ": " + state + " (" + strings.Join(drivers, ", ") + ")"
	}

	var atnDrivers, clkDrivers, dataDrivers, devices []string
	if cia2AtnOut() {
		atnDrivers = append(atnDrivers, "C64")
	}
	if cia2ClkOut() {
		clkDrivers = append(clkDrivers, "C64")
	}
	if cia2DataOut() {
		dataDrivers = append(dataDrivers, "C64")
	}
	for _, p := range iecBus {
		switch p.(type) {
		case *drive1541:
			devices = append(devices, "1541 #8")
			if via1ClkOut() {
				clkDrivers = append(clkDrivers, "1541")
			}
			if via1.DDRB&0x02 != 0 && via1.ORB&0x02 != 0 {
				dataDrivers = append(dataDrivers, "1541:DATA_OUT")
			}
			if ATNAsserted() && !via1AtnAck() {
				dataDrivers = append(dataDrivers, "1541:auto-ack")
			}
		}
	}
	if len(devices) == 0 {
		devices = append(devices, "none")
	}

	return line("ATN", ATNAsserted(), atnDrivers...) + " | " +
		line("CLK", CLKAsserted(), clkDrivers...) + " | " +
		line("DATA", DATAAsserted(), dataDrivers...) + " | " +
		strings.Join(devices, ", ")
}

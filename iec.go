package tiny64

import "strings"

// The IEC serial bus is open-collector: each line is asserted
// (electrically low) if ANY device pulls it low, and only released (high,
// via pull-up) when every device releases it. The C64's CIA2 is the only
// bus driver modeled on this side; attached peripherals (iecPeripheral)
// drive the other side.
//
// CIA2 Port A ($DD00) bits 3/4/5 (ATN/CLOCK/DATA OUT) drive the bus
// through inverting 7406 buffers, so a bit WRITTEN as 1 pulls its line low
// - asserted. That is the convention the KERNAL uses throughout: CLKLO is
// LDA $DD00 / ORA #$10 / STA $DD00, and CLKHI is the matching AND #$EF.
// There is no separate test-harness state: a test harness (or a real CIA2)
// just manipulates CIA2's registers directly via CIA2().

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
	iecActive = true
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
	iecActive = true
	kept := make([]iecPeripheral, 0, len(iecBus))
	for _, existing := range iecBus {
		if existing.iecAddress() != p.iecAddress() {
			kept = append(kept, existing)
		}
	}
	iecBus = kept
}

// addressOccupied reports whether some peripheral already answers for a
// primary address. attachDefaultDrive asks this rather than a dedicated
// flag, because the bus is the thing that can only hold one device per
// address, and only the bus knows about every kind of drive - matching
// upstream's shape (main@6127934) even though this branch has only ever
// had the virtual one.
func addressOccupied(address uint8) bool {
	for _, p := range iecBus {
		if p.iecAddress() == address {
			return true
		}
	}
	return false
}

// iecActive reports whether the bus is worth clocking. Nothing attached
// to it can start a transaction on its own - a generic drive has no clock
// of its own and does nothing but answer - so the only thing that can is
// the C64, and it does so by driving ATN, CLOCK or DATA from CIA2's port
// A. Arming this there, and disarming it when the drive next finds itself
// with nothing to do, is what lets the common case cost one test.
//
// It errs towards armed: attaching or detaching arms it, and so does any
// write to the port, whether or not the bus lines actually moved. An
// unnecessary tick is a wasted cycle, where a missed one is a drive that
// never sees ATN released and a C64 that waits forever for a reply.
var iecActive = true

// iecTick advances every attached peripheral by one Phi2 cycle.
func iecTick() {
	if !iecActive {
		return
	}
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
// it the way the KERNAL does, for tools that drive the bus directly.
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
		switch d := p.(type) {
		case *iecDevice:
			devices = append(devices, "device #"+itoa(int(d.address))+" "+d.stateName())
			if d.clk {
				clkDrivers = append(clkDrivers, "device")
			}
			if d.data {
				dataDrivers = append(dataDrivers, "device")
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

// itoa formats a small non-negative int without pulling in strconv, which
// keeps the TinyGo build lean.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

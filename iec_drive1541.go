//go:build drive1541

package tiny64

// The 1541 side of the IEC bus: VIA1's port B is wired to the serial
// lines, so these translate between the chip's registers and what the bus
// sees. They live here rather than in 1541.go because they are bus
// protocol, not drive mechanics.

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

// describeDrive1541 adds the 1541's contribution to an IECStatus summary,
// reporting whether p was in fact a 1541. It is the counterpart of the
// stub in drive_virtual.go, which is what lets IECStatus name a type that
// a virtual-drive-only build does not compile.
func describeDrive1541(p iecPeripheral, devices, clkDrivers, dataDrivers *[]string) bool {
	if _, ok := p.(*drive1541); !ok {
		return false
	}
	*devices = append(*devices, "1541 #8")
	if via1ClkOut() {
		*clkDrivers = append(*clkDrivers, "1541")
	}
	if via1.DDRB&0x02 != 0 && via1.ORB&0x02 != 0 {
		*dataDrivers = append(*dataDrivers, "1541:DATA_OUT")
	}
	if ATNAsserted() && !via1AtnAck() {
		*dataDrivers = append(*dataDrivers, "1541:auto-ack")
	}
	return true
}

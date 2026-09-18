//go:build drive1541

package tiny64

// The drive's mechanical state - where the head is, whether the motor is
// running, and the raw GCR image of the track under it. The 1541 has no
// index hole, so a track is modelled as a circular buffer of GCR bytes
// that the head walks at the track's bit-cell rate: reads take bytes out
// of it and writes put bytes into it, which is what lets the drive's own
// format routine lay down a track from scratch.
//
// driveHalfTrack follows VICE's convention (2 = track 1.0, so half-track
// = (track-1)*2+2). Only whole tracks are readable - half-tracks exist for
// copy-protection schemes this emulator doesn't support.
var (
	driveHalfTrack        = 2
	driveMotorOn          bool
	driveTrackData        []byte // GCR image of the track under the head
	driveTrackPos         int    // head position within driveTrackData
	driveTrackDirty       bool   // track image holds writes not yet in diskImage
	driveByteReadyCounter int    // Phi2 cycles counted toward the next byte
)

// driveResetDisk returns the head and motor to their power-on state: head
// parked at track 1, motor stopped, no track image cached.
func driveResetDisk() {
	driveFlushTrack()
	driveHalfTrack = 2
	driveMotorOn = false
	driveTrackData = nil
	driveTrackPos = 0
	driveByteReadyCounter = 0
}

// driveTrack returns the whole track number the head is over.
func driveTrack() uint8 {
	return uint8(driveHalfTrack/2 + 1)
}

// trackCyclesPerByte is the bit-cell rate of each of the four density
// zones, in Phi2 cycles per GCR byte. The 1541 spins at 300rpm - 200ms per
// revolution - and writes the outer tracks at a higher bit rate than the
// inner ones, so a byte takes 26 cycles on tracks 1-17 but 32 on tracks
// 31-35, which is what lets the outer tracks hold 21 sectors and the inner
// ones only 17.
func trackCyclesPerByte(track uint8) int {
	switch {
	case track <= 17:
		return 26
	case track <= 24:
		return 28
	case track <= 30:
		return 30
	default:
		return 32
	}
}

// trackCapacity is how many GCR bytes fit on one revolution of a track:
// 200ms at that track's bit-cell rate. This is the number the drive's own
// format routine measures by timing a revolution, and it decides how big
// the inter-sector gaps come out.
func trackCapacity(track uint8) int {
	const cyclesPerRevolution = 200_000 // 300rpm at 1MHz
	return cyclesPerRevolution / trackCyclesPerByte(track)
}

// driveWriteMode reports whether the head is writing rather than reading.
// VIA2's CB2 output selects it: high (%111) reads, low (%110) writes.
func driveWriteMode() bool {
	return via2.pcr&0xE0 == 0xC0
}

// driveByteReadyEnabled reports whether the byte-ready signal is currently
// wired through to the CPU's SO pin. VIA2's CA2 output is the drive's SOE
// line: the DOS drops it (PCR %110) while it isn't interested in bytes, so
// its own polling loops aren't disturbed, and raises it (%111) around the
// code that reads or writes the bitstream.
func driveByteReadyEnabled() bool {
	return via2.pcr&0x0E == 0x0E
}

// ensureTrackData (re)generates the GCR image of the track under the head
// if it's missing, e.g. after a stepper move or a disk change.
func ensureTrackData() {
	if driveTrackData != nil {
		return
	}
	driveTrackData = diskEncodeTrack(driveTrack())
	driveTrackPos = 0
	driveTrackDirty = false
}

// via2StorePRB implements VIA2 Port B's disk-specific side effects
// (stepper motor movement, motor on/off) on top of its normal register
// storage, matching VICE's store_prb.
func via2StorePRB(val uint8) {
	oldPos := driveHalfTrack & 3
	newPos := int(val) & 3
	step := (newPos - oldPos) & 3
	if step == 3 {
		step = -1
	}

	motorOn := val&0x04 != 0
	if motorOn && (step == 1 || step == -1) {
		driveFlushTrack() // whatever was written belongs to the old track
		driveHalfTrack += step
		if driveHalfTrack < 2 {
			driveHalfTrack = 2
		}
		if driveHalfTrack > 80 { // (40-1)*2+2, the highest valid half-track
			driveHalfTrack = 80
		}
		driveTrackData = nil // force regeneration for the new track
	}
	if driveMotorOn && !motorOn {
		driveFlushTrack() // the drive is finished with this track for now
	}
	driveMotorOn = motorOn
}

// via2ReadPRA returns the GCR byte currently under the read head. While
// writing, Port A is an output and simply reads back what the DOS put
// there.
func via2ReadPRA() uint8 {
	if driveWriteMode() {
		return viaEffective(via2.ORA, via2.DDRA)
	}
	ensureTrackData()
	if len(driveTrackData) == 0 {
		return 0
	}
	return driveTrackData[driveTrackPos]
}

// driveAtSync reports whether the byte at pos is part of a SYNC mark.
//
// A SYNC is ten or more consecutive one bits, which is why the drive can
// tell one from data at all: GCR's longest possible run of ones is eight,
// from $5 (01111) followed by $E (11110). Eight is enough to make a whole
// byte $FF when it happens to land on a byte boundary, so "this byte is
// $FF" is not a SYNC test - real data contains such bytes, and treating
// them as SYNC swallows them, which corrupts the block. Two adjacent $FF
// bytes are sixteen ones, which data cannot produce, so a run of at least
// two is the byte-aligned equivalent of the hardware's ten-bit rule. A
// real SYNC is five $FF bytes, so every byte of one is still detected.
func driveAtSync(pos int) bool {
	n := len(driveTrackData)
	if n == 0 || driveTrackData[pos] != 0xFF {
		return false
	}
	prev, next := pos-1, pos+1
	if prev < 0 {
		prev = n - 1
	}
	if next >= n {
		next = 0
	}
	return driveTrackData[prev] == 0xFF || driveTrackData[next] == 0xFF
}

// via2ReadPRB returns Port B's read value: bit 7 (SYNC detected) is 0
// exactly when the byte under the head right now is part of a SYNC mark,
// which can only happen while reading, since the SYNC detector watches the
// read head. Every other bit (stepper/motor/LED/zone/write-protect) uses
// normal DDR-effective read-back, which already yields "not write
// protected" (1) for free since that sense line is configured as an input.
func via2ReadPRB() uint8 {
	base := viaEffective(via2.ORB, via2.DDRB) & 0x7F
	if driveWriteMode() {
		return base | 0x80
	}
	ensureTrackData()
	if driveAtSync(driveTrackPos) {
		return base
	}
	return base | 0x80
}

// via2DiskTick advances the drive's head by one Phi2 cycle. Once a byte's
// worth of bit-cell time has elapsed (and the motor is running) the head
// moves on by one byte: writing deposits VIA2's Port A output onto the
// track first, and either way the CPU's SO pin is pulsed, mirroring the
// real "byte ready" hardware signal that tells the DOS to take the byte
// just read, or to supply the next one to write.
//
// Byte ready is inhibited while the read head is over a SYNC mark, as it
// is on real hardware: the shift register is held cleared for as long as
// the bitstream is all ones, so the first byte the DOS sees after a SYNC
// is the block's ID byte, correctly aligned, rather than a run of $FF.
func via2DiskTick(c *DriveCPU) {
	if !driveMotorOn {
		return
	}
	ensureTrackData()
	if len(driveTrackData) == 0 {
		return
	}

	driveByteReadyCounter++
	if driveByteReadyCounter < trackCyclesPerByte(driveTrack()) {
		return
	}
	driveByteReadyCounter = 0

	writing := driveWriteMode()
	if writing {
		driveTrackData[driveTrackPos] = via2.ORA
		driveTrackDirty = true
	}
	driveTrackPos = (driveTrackPos + 1) % len(driveTrackData)

	if !driveByteReadyEnabled() {
		return
	}
	if writing || !driveAtSync(driveTrackPos) {
		c.SetOverflow()
	}
}

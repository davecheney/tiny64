package tiny64

// The drive's mechanical/head state, driven by writes to VIA2 Port B
// (stepper motor phase, motor on/off) and consumed when generating the
// read bitstream. driveHalfTrack follows VICE's convention (2 = track
// 1.0, so half-track = (track-1)*2+2), though we only ever read whole
// tracks - half-tracks exist for copy-protection schemes this emulator
// doesn't support.
var (
	driveHalfTrack        = 2
	driveMotorOn          bool
	driveTrackData        []byte // synthesized GCR bitstream for the current track
	driveTrackPos         int    // current byte offset within driveTrackData
	driveByteReadyCounter int    // Phi2 cycles counted toward the next byte
)

// driveCyclesPerByte approximates the 1541's bit-cell rate at ~1MHz (all
// density zones use the same fixed rate here, rather than the real
// per-zone timing, since exact bit rate isn't needed for correctness).
const driveCyclesPerByte = 32

// ensureTrackData (re)generates the current track's GCR bitstream if it's
// missing (e.g. after a stepper move), resetting the head to its start.
func ensureTrackData() {
	if driveTrackData != nil {
		return
	}
	track := uint8(driveHalfTrack/2 + 1)
	driveTrackData = diskEncodeTrack(track)
	driveTrackPos = 0
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
		driveHalfTrack += step
		if driveHalfTrack < 0 {
			driveHalfTrack = 0
		}
		if driveHalfTrack > 70 { // (35-1)*2+2, the highest valid half-track
			driveHalfTrack = 70
		}
		driveTrackData = nil // force regeneration for the new track
	}
	driveMotorOn = motorOn
}

// via2ReadPRA returns the GCR byte currently under the read head.
func via2ReadPRA() uint8 {
	ensureTrackData()
	if len(driveTrackData) == 0 {
		return 0
	}
	return driveTrackData[driveTrackPos]
}

// via2ReadPRB returns Port B's read value: bit 7 (SYNC detected) is 0
// exactly when the byte under the head right now is part of a SYNC mark;
// every other bit (stepper/motor/LED/zone/write-protect) uses normal
// DDR-effective read-back, which already yields "not write protected"
// (1) for free since that sense line is configured as an input.
func via2ReadPRB() uint8 {
	ensureTrackData()
	base := viaEffective(via2.ORB, via2.DDRB) & 0x7F
	if len(driveTrackData) > 0 && driveTrackData[driveTrackPos] == 0xFF {
		return base
	}
	return base | 0x80
}

// via2DiskTick advances the drive's read head by one Phi2 cycle: once
// enough cycles have elapsed for one GCR byte's bit-cell time (and the
// drive motor is running), it advances to the next byte and pulses the
// CPU's SO pin, mirroring the real "byte ready" hardware signal.
func via2DiskTick(c *DriveCPU) {
	if !driveMotorOn {
		return
	}
	ensureTrackData()
	if len(driveTrackData) == 0 {
		return
	}
	driveByteReadyCounter++
	if driveByteReadyCounter >= driveCyclesPerByte {
		driveByteReadyCounter = 0
		driveTrackPos = (driveTrackPos + 1) % len(driveTrackData)
		c.SetOverflow()
	}
}

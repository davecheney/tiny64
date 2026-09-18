//go:build !drive1541

package tiny64

// This is the drive seam for a build without the physical 1541. The
// virtual drive answers device 8 instead: it speaks the serial protocol on
// the wire but implements CBM DOS in Go, so there is no second 6502 to
// step and no GCR to decode.
//
// That is the whole point of the tag. iecTick walks every attached
// peripheral once per Phi2 cycle, and a 1541 on the bus means a complete
// second 6502 plus two 6522 VIAs on every one of a frame's 19,656 cycles -
// unconditionally, because the DOS ROM idles in an ATN polling loop and
// never sleeps. Measured on the demo fixtures that is +41.7% a frame,
// against +3.9% for the virtual drive.
//
// Build with -tags drive1541 to get the real thing, which is what
// fastloaders, copy protection and drive-code upload need.

// Has1541 reports whether the physical 1541 is compiled into this build.
// It is a constant, so `if Has1541` folds away entirely and the guarded
// code carries no runtime cost in either configuration.
const Has1541 = false

// attachDefaultDrive puts whichever drive this build has on the bus at
// device 8, if nothing is answering there already. See the drive1541
// counterpart in drive_real.go for why the guard is bus occupancy rather
// than a per-drive flag - here it also keeps InsertDisk from tearing down
// a drive attached by AttachVirtualPRG, which holds its file in memory.
func attachDefaultDrive() {
	if addressOccupied(8) {
		return
	}
	AttachVirtualDrive(8)
}

// resetDriveIfAttached does nothing without a drive CPU to reset. The
// virtual drive keeps no state that survives a C64 reset.
func resetDriveIfAttached() {}

// driveFlushTrack writes back whatever the head has written to the track
// under it. Without a head there is nothing to write back: the virtual
// drive edits the disk image directly.
func driveFlushTrack() {}

// driveDropTrackCache forgets the GCR image of the track under the head.
// There is no such image without a drive that decodes GCR.
func driveDropTrackCache() {}

// describeDrive1541 is the stub half of IECStatus's 1541 arm. A build
// without the physical drive has no such type on the bus, so this always
// reports "not a 1541" and IECStatus falls through to its other cases.
func describeDrive1541(iecPeripheral, *[]string, *[]string, *[]string) bool {
	return false
}

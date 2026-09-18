//go:build drive1541

package tiny64

// Has1541 reports whether the physical 1541 is compiled into this build.
// It is a constant, so `if Has1541` folds away entirely and the guarded
// code carries no runtime cost in either configuration.
const Has1541 = true

// attachDefaultDrive puts whichever drive this build has on the bus at
// device 8, if nothing is answering there already. InsertDisk calls it, so
// "put a disk in" attaches a drive without the caller having to know which
// kind was compiled in.
//
// The guard is bus occupancy rather than a per-drive flag: the bus is what
// can only hold one device per address, and it is the only thing that
// knows about every kind of drive. Asking a 1541-specific flag would let
// this displace a virtual drive somebody had already attached.
func attachDefaultDrive() {
	if addressOccupied(8) {
		return
	}
	AttachDrive(true)
}

// resetDriveIfAttached resets the drive along with the C64, the way the
// two power up together. A drive that is not attached has nothing to
// reset.
func resetDriveIfAttached() {
	if driveAttached {
		ResetDrive()
	}
}

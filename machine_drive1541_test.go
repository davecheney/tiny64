//go:build drive1541

package tiny64

// saveDriveState snapshots the physical 1541's singletons and returns the
// closure that puts them back, along with the disk that was in it.
//
// The order inside that closure is the order saveMachine used to spell out
// inline, and it matters: the drive's own state goes back first so that
// the flush InsertDisk performs writes through restored registers, then
// the disk, then driveResetDisk drops the track image the test left under
// the head, and driveAttached goes back last because InsertDisk will have
// attached a drive of its own on the way past.
func saveDriveState() func(disk []byte) {
	savedDriveCPU, savedVIA1, savedVIA2 := driveCPU, via1, via2
	savedDriveRAM := driveRAM
	savedDriveAttached := driveAttached
	return func(disk []byte) {
		driveCPU, via1, via2 = savedDriveCPU, savedVIA1, savedVIA2
		driveRAM = savedDriveRAM
		InsertDisk(disk)
		driveResetDisk()
		driveAttached = savedDriveAttached
	}
}

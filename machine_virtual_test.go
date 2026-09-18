//go:build !drive1541

package tiny64

// saveDriveState is the virtual drive's half of saveMachine. There is no
// drive CPU, no VIAs and no drive RAM in this build, and the virtual drive
// keeps nothing that outlives the bus entry saveMachine already restores -
// so putting the disk back is the whole job.
func saveDriveState() func(disk []byte) {
	return func(disk []byte) { InsertDisk(disk) }
}

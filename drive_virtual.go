package tiny64

// This branch has only ever had the generic drive: it speaks the IEC
// serial protocol cycle by cycle but implements CBM DOS in Go against a
// D64 image, with no second 6502 to step and no GCR to decode. Unlike
// upstream (main@6127934), there is no drive_real.go counterpart here and
// no -tags drive1541 build - the real 1541 (its own CPU, two 6522 VIAs,
// GCR codec, 16K DOS ROM) has been deliberately kept off this lightweight
// branch, per the branch's own stewardship log.
//
// Has1541 and attachDefaultDrive exist anyway, under upstream's own names,
// so that future commits landing on top of the 6127934 seam - such as
// iecTick cost reductions - can still be read and adapted against matching
// symbols here, even without the other half of the seam existing.

// Has1541 reports whether the physical 1541 is compiled into this build.
// Always false on this branch; kept as a named constant (rather than an
// inline literal) so callers read the same way upstream's do.
const Has1541 = false

// attachDefaultDrive puts the drive this build has on the bus at device 8,
// if nothing is answering there already. InsertDisk calls it, matching
// upstream's shape: the guard is bus occupancy, not a dedicated flag, so
// it does not displace a peripheral (such as AttachVirtualPRG's) that got
// there first.
func attachDefaultDrive() {
	if addressOccupied(8) {
		return
	}
	AttachVirtualDrive(8)
}

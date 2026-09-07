package tiny64

var ram [65536]byte

func Ram() []byte {
	return ram[:]
}

// colorRAM models the 2114 SRAM chip that backs the VIC-II's per-character
// colour nibble at $D800-$DBFF. Unlike the rest of the address space, this
// is not a window into the 64K system RAM: the 2114 is a physically
// separate 1024x4-bit chip, so it only stores 4 bits per location. When
// I/O is banked out (e.g. an all-RAM CPU port configuration), $D800-$DBFF
// is backed by ordinary system RAM instead - a different chip entirely -
// which is why this array is kept independent of ram rather than aliased
// onto it.
var colorRAM [1024]byte

// ColorRam returns the low nibble of each of the 1024 colour RAM cells.
func ColorRam() []byte {
	return colorRAM[:]
}

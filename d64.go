package tiny64

// diskImage holds the raw bytes of the currently inserted D64 disk image
// (standard 35-track, no error info, 174848 bytes); nil means no disk.
var diskImage []byte

// InsertDisk loads a raw D64 disk image for the drive to read.
func InsertDisk(data []byte) {
	diskImage = data
}

// sectorsPerTrack returns the standard D64 sector count for track (1-35,
// non-extended format); 0 for an out-of-range track.
func sectorsPerTrack(track uint8) int {
	switch {
	case track >= 1 && track <= 17:
		return 21
	case track >= 18 && track <= 24:
		return 19
	case track >= 25 && track <= 30:
		return 18
	case track >= 31 && track <= 35:
		return 17
	default:
		return 0
	}
}

// trackOffset returns the byte offset of the start of track (1-35) within
// a standard D64 image.
func trackOffset(track uint8) int {
	offset := 0
	for t := uint8(1); t < track; t++ {
		offset += sectorsPerTrack(t) * 256
	}
	return offset
}

// diskReadSector returns the raw 256 bytes at (track, sector), or nil if
// out of range or no disk is inserted.
func diskReadSector(track, sector uint8) *[256]byte {
	n := sectorsPerTrack(track)
	if diskImage == nil || n == 0 || int(sector) >= n {
		return nil
	}
	offset := trackOffset(track) + int(sector)*256
	if offset+256 > len(diskImage) {
		return nil
	}
	var block [256]byte
	copy(block[:], diskImage[offset:offset+256])
	return &block
}

// diskID returns the two disk ID bytes stored in the BAM sector (track 18,
// sector 0, offset $A2/$A3), used for GCR sector-header verification.
func diskID() (id1, id2 uint8) {
	bam := diskReadSector(18, 0)
	if bam == nil {
		return 0, 0
	}
	return bam[0xA2], bam[0xA3]
}

// diskEncodeTrack builds the full GCR bitstream for one physical rotation
// of the given track, concatenating every sector's on-disk byte sequence
// (SYNC/header/gap/SYNC/data) in order.
func diskEncodeTrack(track uint8) []byte {
	n := sectorsPerTrack(track)
	id1, id2 := diskID()
	var buf []byte
	for s := 0; s < n; s++ {
		block := diskReadSector(track, uint8(s))
		if block == nil {
			block = &[256]byte{}
		}
		buf = gcrEncodeSector(buf, track, uint8(s), id1, id2, block)
	}
	return buf
}

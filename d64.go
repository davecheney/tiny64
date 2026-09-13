package tiny64

// diskImage holds the raw bytes of the currently inserted D64 disk image
// (standard 35-track or 40-track, no error info); nil means no disk.
var diskImage []byte

// D64Size is the length of a standard 35-track D64 image with no error
// info: 683 sectors of 256 bytes.
const D64Size = 174848

// D64Size40 is the length of a standard 40-track D64 image with no error
// info: 768 sectors of 256 bytes.
const D64Size40 = 196608

// InsertDisk loads a raw D64 disk image for the drive to read and write;
// nil ejects whatever was in the drive.
func InsertDisk(data []byte) {
	if data != nil && !virtualDriveAttached {
		AttachVirtualDrive(8) // a disk needs something to put it in
	}
	diskImage = data
}

// NewDisk returns an empty, unformatted 35-track D64 image, ready for the
// drive's own NEW (format) command to lay tracks down on.
func NewDisk() []byte {
	return make([]byte, D64Size)
}

// DiskImage returns the disk image currently in the drive. The image
// aliases the drive's own copy, so callers that intend to keep it should
// take their own copy.
func DiskImage() []byte {
	return diskImage
}

// DiskInserted reports whether a disk image is currently in the drive.
func DiskInserted() bool { return diskImage != nil }

// sectorsPerTrack returns the standard D64 sector count for track (1-40);
// 0 for an out-of-range track.
func sectorsPerTrack(track uint8) int {
	switch {
	case track >= 1 && track <= 17:
		return 21
	case track >= 18 && track <= 24:
		return 19
	case track >= 25 && track <= 30:
		return 18
	case track >= 31 && track <= 40:
		return 17
	default:
		return 0
	}
}

// trackOffset returns the byte offset of the start of track (1-40) within
// a standard D64 image.
func trackOffset(track uint8) int {
	offset := 0
	for t := uint8(1); t < track; t++ {
		offset += sectorsPerTrack(t) * 256
	}
	return offset
}

// sectorOffset returns the byte offset of (track, sector) within a
// D64 image, or -1 if either is out of range or the image is too
// short to hold it.
func sectorOffset(track, sector uint8) int {
	n := sectorsPerTrack(track)
	if diskImage == nil || n == 0 || int(sector) >= n {
		return -1
	}
	offset := trackOffset(track) + int(sector)*256
	if offset+256 > len(diskImage) {
		return -1
	}
	return offset
}

// diskReadSector returns the raw 256 bytes at (track, sector), or nil if
// out of range or no disk is inserted.
func diskReadSector(track, sector uint8) *[256]byte {
	offset := sectorOffset(track, sector)
	if offset < 0 {
		return nil
	}
	var block [256]byte
	copy(block[:], diskImage[offset:offset+256])
	return &block
}

// diskWriteSector stores 256 bytes at (track, sector), reporting whether
// the sector exists on a standard D64.
func diskWriteSector(track, sector uint8, data []byte) bool {
	offset := sectorOffset(track, sector)
	if offset < 0 {
		return false
	}
	copy(diskImage[offset:offset+256], data)
	return true
}

// diskID returns the two ID bytes stamped into this disk's sector headers,
// taken from the BAM sector (track 18, sector 0, offset $A2/$A3) the way
// every other tool does.
func diskID() (id1, id2 uint8) {
	bam := diskReadSector(18, 0)
	if bam == nil {
		return 0, 0
	}
	return bam[0xA2], bam[0xA3]
}

package tiny64

// diskImage holds the raw bytes of the currently inserted D64 disk image
// (standard 35-track or 40-track, no error info); nil means no disk.
var diskImage []byte

// diskFormatID remembers the ID bytes the drive itself last stamped into a
// sector header, which take precedence over the BAM's copy until the disk
// is changed. See diskID.
var (
	diskFormatID      [2]uint8
	diskFormatIDValid bool
)

// D64Size is the length of a standard 35-track D64 image with no error
// info: 683 sectors of 256 bytes.
const D64Size = 174848

// D64Size40 is the length of a standard 40-track D64 image with no error
// info: 768 sectors of 256 bytes.
const D64Size40 = 196608

// InsertDisk loads a raw D64 disk image for the drive to read and write;
// nil ejects whatever was in the drive. Either way the cached GCR track
// image is dropped, so the head starts on the new disk from its next
// revolution - the emulator's equivalent of the write-protect switch
// flicking as the flap opens, which is how a real drive notices.
func InsertDisk(data []byte) {
	if data != nil && !driveAttached {
		AttachDrive(true) // a disk needs something to put it in
	}
	replaceDisk(data)
}

// replaceDisk changes the media without changing which IEC device owns it.
// The generic drive uses this for its NEW command; calling InsertDisk there
// would attach a 1541 in the middle of the command-channel transaction.
func replaceDisk(data []byte) {
	driveFlushTrack()
	diskImage = data
	driveTrackData = nil
	driveTrackPos = 0
	driveTrackDirty = false
	diskFormatIDValid = false
}

// NewDisk returns an empty, unformatted 35-track D64 image, ready for the
// drive's own NEW (format) command to lay tracks down on.
func NewDisk() []byte {
	return make([]byte, D64Size)
}

// DiskImage returns the disk image currently in the drive, first writing
// back anything the drive has written to the track under its head. The
// image aliases the drive's own copy, so callers that intend to keep it
// should take their own copy.
func DiskImage() []byte {
	driveFlushTrack()
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

// diskID returns the two ID bytes stamped into this disk's sector headers.
// A D64 has nowhere to keep them, so they are taken from the BAM sector
// (track 18, sector 0, offset $A2/$A3) the way every other tool does -
// unless the drive has since formatted the disk, in which case the ID it
// wrote is what the medium now carries. That distinction matters during a
// format: the DOS stamps every track with the new ID long before it gets
// around to writing a matching BAM, and would trip over its own ID check
// if regenerated tracks still carried the old one.
func diskID() (id1, id2 uint8) {
	if diskFormatIDValid {
		return diskFormatID[0], diskFormatID[1]
	}
	bam := diskReadSector(18, 0)
	if bam == nil {
		return 0, 0
	}
	return bam[0xA2], bam[0xA3]
}

// diskEncodeTrack builds the GCR image of one full revolution of the given
// track: every sector's on-disk byte sequence (SYNC/header/gap/SYNC/data)
// in order, with the leftover space shared out as inter-sector tail gaps
// so the sectors fill the track exactly, the way a real formatted disk
// does.
func diskEncodeTrack(track uint8) []byte {
	n := sectorsPerTrack(track)
	if n == 0 {
		return nil
	}
	capacity := trackCapacity(track)

	// Whatever is left after the sectors themselves becomes gap. Sharing
	// it out evenly leaves a remainder, which goes into the last gap -
	// exactly what the drive's format routine does, since it can't know
	// where the track ends until it comes back around to the first SYNC.
	gap := (capacity - n*gcrSectorLen) / n
	remainder := capacity - n*(gcrSectorLen+gap)

	id1, id2 := diskID()
	buf := make([]byte, 0, capacity)
	for s := range n {
		block := diskReadSector(track, uint8(s))
		if block == nil {
			block = &[256]byte{}
		}
		tail := gap
		if s == n-1 {
			tail += remainder
		}
		buf = gcrEncodeSector(buf, track, uint8(s), id1, id2, block, tail)
	}
	return buf
}

// driveFlushTrack writes the track image under the head back into the D64,
// if the drive has written to it. This is where a formatted or rewritten
// track stops being a GCR bitstream and becomes sectors again: the track
// is scanned for SYNC marks exactly as the drive's own read logic does,
// and every header/data block pair that decodes cleanly is stored at the
// track and sector its header claims.
func driveFlushTrack() {
	if !driveTrackDirty || diskImage == nil {
		return
	}
	driveTrackDirty = false

	var haveHeader bool
	var headerTrack, headerSector uint8
	for _, pos := range driveTrackBlocks() {
		id, ok := driveTrackBlockID(pos)
		if !ok {
			continue
		}
		switch {
		case id == gcrHeaderID:
			raw, ok := gcrDecodeBlock(driveTrackData, pos, 8)
			if !ok || raw[1] != raw[2]^raw[3]^raw[4]^raw[5] {
				haveHeader = false
				continue
			}
			headerSector, headerTrack = raw[2], raw[3]
			diskFormatID, diskFormatIDValid = [2]uint8{raw[5], raw[4]}, true
			haveHeader = true
		case id == gcrDataID && haveHeader:
			haveHeader = false
			raw, ok := gcrDecodeBlock(driveTrackData, pos, 260)
			if !ok {
				continue
			}
			var checksum uint8
			for _, b := range raw[1:257] {
				checksum ^= b
			}
			if checksum != raw[257] {
				continue
			}
			diskWriteSector(headerTrack, headerSector, raw[1:257])
		}
	}
}

// driveTrackBlocks returns the offset of the first byte after each SYNC
// mark in the track image - that is, the start of every block the drive
// would be able to read. Because the track is circular, a SYNC that
// straddles the end of the buffer is found too.
func driveTrackBlocks() []int {
	n := len(driveTrackData)
	if n == 0 {
		return nil
	}
	var blocks []int
	inSync := driveTrackData[n-1] == 0xFF
	for i := range n {
		switch {
		case driveTrackData[i] == 0xFF:
			inSync = true
		case inSync:
			inSync = false
			blocks = append(blocks, i)
		}
	}
	return blocks
}

// driveTrackBlockID decodes just enough of the block starting at pos to
// recover the block identifier byte that says whether a header or a data
// block follows.
func driveTrackBlockID(pos int) (uint8, bool) {
	n := len(driveTrackData)
	var group [5]byte
	for i := range group {
		group[i] = driveTrackData[(pos+i)%n]
	}
	dest, ok := gcrDecode5Bytes(group)
	return dest[0], ok
}

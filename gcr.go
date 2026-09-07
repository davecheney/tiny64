package tiny64

// gcrEncodeTable maps a 4-bit nibble to its 5-bit GCR code, the standard
// Commodore 1541 encoding chosen so no code has more than 2 consecutive
// zero bits (keeping the bitstream self-clocking) and no code looks like
// the SYNC mark (10+ consecutive 1 bits).
var gcrEncodeTable = [16]uint8{
	0x0a, 0x0b, 0x12, 0x13,
	0x0e, 0x0f, 0x16, 0x17,
	0x09, 0x19, 0x1a, 0x1b,
	0x0d, 0x1d, 0x1e, 0x15,
}

// gcrEncode4Bytes converts 4 raw bytes into 5 GCR-encoded bytes: each of
// the 8 nibbles becomes a 5-bit GCR code, and the resulting 40 bits are
// packed msb-first into 5 bytes.
func gcrEncode4Bytes(source [4]byte) [5]byte {
	var tdest uint32
	var dest [5]byte
	for i, si := 2, 0; si < 4; i, si = i+2, si+1 {
		tdest <<= 5
		tdest |= uint32(gcrEncodeTable[source[si]>>4])
		tdest <<= 5
		tdest |= uint32(gcrEncodeTable[source[si]&0x0F])
		dest[si] = byte(tdest >> uint(i))
	}
	dest[4] = byte(tdest)
	return dest
}

// gcrHeaderGap/gcrDataGap/gcrSyncLen/gcrTailGap are reasonable fixed gap
// sizes (in bytes) for the synthesized bitstream; real hardware varies the
// tail gap per track to make sectors fit evenly, but since we generate the
// stream on the fly rather than persisting a physical track image, a
// fixed small gap is enough for the drive to reliably find each SYNC.
const (
	gcrSyncLen   = 5 // bytes of $FF (marks a SYNC: 40 consecutive 1 bits)
	gcrHeaderGap = 9 // gap bytes between the header block and the data SYNC
	gcrTailGap   = 8 // gap bytes between one sector's data block and the next SYNC
)

// gcrEncodeSector appends one sector's full on-disk byte sequence (SYNC,
// GCR-encoded header, gap, SYNC, GCR-encoded 256-byte data block, tail
// gap) to dst, matching the real 1541 disk format (see VICE's
// gcr_convert_sector_to_GCR). id1/id2 are the disk ID bytes stored in
// every sector header for verification.
func gcrEncodeSector(dst []byte, track, sector, id1, id2 uint8, data *[256]byte) []byte {
	for range gcrSyncLen {
		dst = append(dst, 0xFF)
	}

	headerChecksum := sector ^ track ^ id2 ^ id1
	h1 := gcrEncode4Bytes([4]byte{0x08, headerChecksum, sector, track})
	dst = append(dst, h1[:]...)
	h2 := gcrEncode4Bytes([4]byte{id2, id1, 0x0F, 0x0F})
	dst = append(dst, h2[:]...)

	for range gcrHeaderGap {
		dst = append(dst, 0x55)
	}

	for range gcrSyncLen {
		dst = append(dst, 0xFF)
	}

	checksum := data[0] ^ data[1] ^ data[2]
	d := gcrEncode4Bytes([4]byte{0x07, data[0], data[1], data[2]})
	dst = append(dst, d[:]...)
	for i := 1; i <= 63; i++ {
		b := i*4 - 1 // data[3], data[7], ...
		checksum ^= data[b] ^ data[b+1] ^ data[b+2] ^ data[b+3]
		d = gcrEncode4Bytes([4]byte{data[b], data[b+1], data[b+2], data[b+3]})
		dst = append(dst, d[:]...)
	}
	last := data[255]
	d = gcrEncode4Bytes([4]byte{last, checksum ^ last, 0, 0})
	dst = append(dst, d[:]...)

	for range gcrTailGap {
		dst = append(dst, 0x55)
	}

	return dst
}

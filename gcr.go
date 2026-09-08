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

// gcrInvalid marks a 5-bit code with no nibble equivalent.
const gcrInvalid = 0xFF

// gcrDecodeTable is the inverse of gcrEncodeTable: it maps a 5-bit GCR
// code back to its nibble, or gcrInvalid for the 16 codes no encoder ever
// produces (which therefore mean the bitstream is misaligned or corrupt).
var gcrDecodeTable = buildGCRDecodeTable()

func buildGCRDecodeTable() [32]uint8 {
	var t [32]uint8
	for i := range t {
		t[i] = gcrInvalid
	}
	for nibble, code := range gcrEncodeTable {
		t[code] = uint8(nibble)
	}
	return t
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

// gcrDecode5Bytes reverses gcrEncode4Bytes, converting 5 GCR bytes back
// into the 4 raw bytes they were made from. ok is false if any of the
// eight 5-bit groups isn't a valid GCR code, which is how a caller scanning
// a track tells real data from gap filler.
func gcrDecode5Bytes(source [5]byte) (dest [4]byte, ok bool) {
	var bits uint64
	for _, b := range source {
		bits = bits<<8 | uint64(b)
	}
	ok = true
	for i := range 8 {
		nibble := gcrDecodeTable[uint8(bits>>uint(35-5*i))&0x1F]
		if nibble == gcrInvalid {
			ok = false
			nibble = 0
		}
		if i%2 == 0 {
			dest[i/2] = nibble << 4
		} else {
			dest[i/2] |= nibble
		}
	}
	return dest, ok
}

// The on-disk layout of one sector, in bytes: a SYNC mark, the GCR-encoded
// 8-byte header block, a header gap, a second SYNC, the GCR-encoded
// 260-byte data block, and finally a tail gap before the next sector's
// SYNC. Real hardware stretches the tail gap so the sectors fill exactly
// one revolution, which diskEncodeTrack reproduces; the rest are the fixed
// sizes the 1541's own format routine writes.
const (
	gcrSyncLen   = 5   // bytes of $FF (a SYNC is 10+ consecutive 1 bits)
	gcrHeaderLen = 10  // GCR-encoded header block (8 raw bytes)
	gcrHeaderGap = 9   // gap between the header block and the data SYNC
	gcrDataLen   = 325 // GCR-encoded data block (260 raw bytes)

	// gcrSectorLen is one sector's on-disk length excluding the tail gap.
	gcrSectorLen = gcrSyncLen + gcrHeaderLen + gcrHeaderGap + gcrSyncLen + gcrDataLen

	// gcrGapByte is the filler written into gaps. Its bit pattern is not a
	// valid GCR code, so a reader that lands in a gap can tell.
	gcrGapByte = 0x55

	// gcrHeaderID/gcrDataID are the first raw byte of a header and a data
	// block respectively, which is how the drive tells the two apart.
	gcrHeaderID = 0x08
	gcrDataID   = 0x07
)

// gcrEncodeSector appends one sector's full on-disk byte sequence (SYNC,
// GCR-encoded header, gap, SYNC, GCR-encoded 256-byte data block, tail
// gap) to dst, matching the real 1541 disk format (see VICE's
// gcr_convert_sector_to_GCR). id1/id2 are the disk ID bytes stored in
// every sector header for verification.
func gcrEncodeSector(dst []byte, track, sector, id1, id2 uint8, data *[256]byte, tailGap int) []byte {
	for range gcrSyncLen {
		dst = append(dst, 0xFF)
	}

	headerChecksum := sector ^ track ^ id2 ^ id1
	h1 := gcrEncode4Bytes([4]byte{gcrHeaderID, headerChecksum, sector, track})
	dst = append(dst, h1[:]...)
	h2 := gcrEncode4Bytes([4]byte{id2, id1, 0x0F, 0x0F})
	dst = append(dst, h2[:]...)

	for range gcrHeaderGap {
		dst = append(dst, gcrGapByte)
	}

	for range gcrSyncLen {
		dst = append(dst, 0xFF)
	}

	checksum := data[0] ^ data[1] ^ data[2]
	d := gcrEncode4Bytes([4]byte{gcrDataID, data[0], data[1], data[2]})
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

	for range tailGap {
		dst = append(dst, gcrGapByte)
	}

	return dst
}

// gcrDecodeBlock decodes n raw bytes from a GCR block that starts at pos
// in the circular track image track, reversing gcrEncodeSector's packing.
// n must be a multiple of 4. ok is false if the block contains a code that
// isn't valid GCR.
func gcrDecodeBlock(track []byte, pos, n int) (raw []byte, ok bool) {
	raw = make([]byte, 0, n)
	ok = true
	for len(raw) < n {
		var group [5]byte
		for i := range group {
			group[i] = track[(pos+i)%len(track)]
		}
		pos += 5
		dec, valid := gcrDecode5Bytes(group)
		if !valid {
			ok = false
		}
		raw = append(raw, dec[:]...)
	}
	return raw[:n], ok
}

package tiny64

import "testing"

// TestD64TrackLayout checks the standard 35-track D64 sector-count zones
// and that the cumulative track offsets add up to the well-known 174848
// byte image size (35 tracks, no error info).
func TestD64TrackLayout(t *testing.T) {
	total := 0
	for track := uint8(1); track <= 35; track++ {
		if trackOffset(track) != total {
			t.Fatalf("trackOffset(%d) = %d, want %d", track, trackOffset(track), total)
		}
		total += sectorsPerTrack(track) * 256
	}
	const want = 174848
	if total != want {
		t.Fatalf("total D64 size = %d, want %d", total, want)
	}
}

// TestDiskReadSector checks that a disk image's bytes land at the
// expected (track,sector) via diskReadSector.
func TestDiskReadSector(t *testing.T) {
	defer func() { diskImage = nil }()

	data := make([]byte, 174848)
	// Mark track 1 sector 0's first byte, and track 18 sector 0 (BAM)'s
	// disk ID bytes.
	data[0] = 0xAA
	bamOffset := trackOffset(18)
	data[bamOffset+0xA2] = 0x41
	data[bamOffset+0xA3] = 0x42
	InsertDisk(data)

	block := diskReadSector(1, 0)
	if block == nil || block[0] != 0xAA {
		t.Fatalf("diskReadSector(1,0)[0] = %v, want 0xAA", block)
	}

	id1, id2 := diskID()
	if id1 != 0x41 || id2 != 0x42 {
		t.Fatalf("diskID() = %#02x,%#02x, want 0x41,0x42", id1, id2)
	}

	if diskReadSector(1, 21) != nil {
		t.Fatalf("diskReadSector(1,21) should be nil (track 1 only has 21 sectors, 0-20)")
	}
	if diskReadSector(36, 0) != nil {
		t.Fatalf("diskReadSector(36,0) should be nil (track out of range)")
	}
}

// TestDiskEncodeTrack checks that a full track's synthesized GCR
// bitstream has the expected length (one gcrEncodeSector's worth of bytes
// per sector) and that each sector's header decodes to the right
// track/sector/ID.
func TestDiskEncodeTrack(t *testing.T) {
	defer func() { diskImage = nil }()

	data := make([]byte, 174848)
	bamOffset := trackOffset(18)
	data[bamOffset+0xA2] = 0x41
	data[bamOffset+0xA3] = 0x42
	InsertDisk(data)

	const track = 1
	bitstream := diskEncodeTrack(track)

	sectorLen := gcrSyncLen + 5 + 5 + gcrHeaderGap + gcrSyncLen + 5*65 + gcrTailGap
	wantLen := sectorLen * sectorsPerTrack(track)
	if len(bitstream) != wantLen {
		t.Fatalf("len(diskEncodeTrack(%d)) = %d, want %d", track, len(bitstream), wantLen)
	}

	for s := 0; s < sectorsPerTrack(track); s++ {
		base := s * sectorLen
		headerStart := base + gcrSyncLen
		header := gcrDecode4Bytes([5]byte{
			bitstream[headerStart], bitstream[headerStart+1], bitstream[headerStart+2],
			bitstream[headerStart+3], bitstream[headerStart+4],
		})
		if header[0] != 0x08 {
			t.Fatalf("sector %d: header block ID = %#02x, want 0x08", s, header[0])
		}
		if header[2] != uint8(s) || header[3] != track {
			t.Fatalf("sector %d: header sector/track = %d/%d, want %d/%d", s, header[2], header[3], s, track)
		}
	}
}

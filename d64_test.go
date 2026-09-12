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
	// InsertDisk plugs in a 1541 as a side effect, so this needs the full
	// machine snapshot to put the bus back, not just diskImage.
	saveMachine(t)

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
	// InsertDisk(nil) does not undo the drive InsertDisk(data) attached.
	saveMachine(t)

	data := make([]byte, D64Size)
	bamOffset := trackOffset(18)
	data[bamOffset+0xA2] = 0x41
	data[bamOffset+0xA3] = 0x42
	InsertDisk(data)

	const track = 1
	bitstream := diskEncodeTrack(track)

	// A track image is exactly one revolution long, whatever that track's
	// bit-cell rate makes it: the sectors take what they take and the gaps
	// absorb the rest.
	if len(bitstream) != trackCapacity(track) {
		t.Fatalf("len(diskEncodeTrack(%d)) = %d, want one revolution (%d)", track, len(bitstream), trackCapacity(track))
	}

	// Every block on the track is whatever follows a SYNC mark, which is
	// how the drive itself finds them.
	saveTrack := driveTrackData
	defer func() { driveTrackData = saveTrack }()
	driveTrackData = bitstream

	var headers int
	for _, pos := range driveTrackBlocks() {
		raw, ok := gcrDecodeBlock(bitstream, pos, 8)
		if !ok {
			t.Fatalf("block at %d does not decode as GCR", pos)
		}
		if raw[0] != gcrHeaderID {
			continue
		}
		if raw[2] != uint8(headers) || raw[3] != track {
			t.Fatalf("block at %d: header sector/track = %d/%d, want %d/%d", pos, raw[2], raw[3], headers, track)
		}
		headers++
	}
	if want := sectorsPerTrack(track); headers != want {
		t.Fatalf("found %d sector headers on track %d, want %d", headers, track, want)
	}
}

func TestMakeD64FromPRG(t *testing.T) {
	saveMachine(t)

	prgData := []byte{0x01, 0x08, 0x00, 0x00} // $0801 start address + dummy BASIC bytes
	d64, err := MakeD64FromPRG("love.prg", prgData)
	if err != nil {
		t.Fatalf("MakeD64FromPRG failed: %v", err)
	}
	if len(d64) != D64Size {
		t.Fatalf("len(d64) = %d, want %d", len(d64), D64Size)
	}

	InsertDisk(d64)
	entries := diskDirectory()
	if len(entries) != 1 {
		t.Fatalf("len(diskDirectory()) = %d, want 1", len(entries))
	}

	if name := entries[0].nameString(); name != "LOVE" {
		t.Errorf("entry name = %q, want %q", name, "LOVE")
	}

	if typ := entries[0].fileType(); typ != ftypePRG {
		t.Errorf("entry type = %d, want ftypePRG (%d)", typ, ftypePRG)
	}

	readData := diskReadFile(entries[0])
	if string(readData) != string(prgData) {
		t.Errorf("diskReadFile = %v, want %v", readData, prgData)
	}
}

package tiny64

import (
	"os"
	"path/filepath"
	"testing"
)

// TestD64TrackLayout checks the standard 35-track and 40-track D64 sector-count zones
// and that the cumulative track offsets add up to the well-known 174848
// and 196608 byte image sizes (35 and 40 tracks, no error info).
func TestD64TrackLayout(t *testing.T) {
	total := 0
	for track := uint8(1); track <= 35; track++ {
		if trackOffset(track) != total {
			t.Fatalf("trackOffset(%d) = %d, want %d", track, trackOffset(track), total)
		}
		total += sectorsPerTrack(track) * 256
	}
	if total != D64Size {
		t.Fatalf("total 35-track D64 size = %d, want %d", total, D64Size)
	}

	for track := uint8(36); track <= 40; track++ {
		if trackOffset(track) != total {
			t.Fatalf("trackOffset(%d) = %d, want %d", track, trackOffset(track), total)
		}
		total += sectorsPerTrack(track) * 256
	}
	if total != D64Size40 {
		t.Fatalf("total 40-track D64 size = %d, want %d", total, D64Size40)
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

// TestDisk40TrackAccess verifies sector reading/writing on tracks 36-40
// for a 196608-byte D64 image.
func TestDisk40TrackAccess(t *testing.T) {
	saveMachine(t)

	// 35-track image should reject track 36
	data35 := make([]byte, D64Size)
	InsertDisk(data35)
	if diskReadSector(36, 0) != nil {
		t.Fatalf("diskReadSector(36, 0) on 35-track image should be nil")
	}

	// 40-track image (196608 bytes)
	data40 := make([]byte, D64Size40)
	// Mark track 40 sector 16 (last sector of track 40)
	t40s16Offset := trackOffset(40) + 16*256
	data40[t40s16Offset] = 0x55
	data40[t40s16Offset+255] = 0xAA
	InsertDisk(data40)

	// Test track 36 sector 0
	if !diskWriteSector(36, 0, []byte{0x12, 0x34}) {
		t.Fatalf("diskWriteSector(36, 0) failed on 40-track image")
	}
	blk36 := diskReadSector(36, 0)
	if blk36 == nil || blk36[0] != 0x12 || blk36[1] != 0x34 {
		t.Fatalf("diskReadSector(36, 0) = %v, want [0x12, 0x34...]", blk36)
	}

	// Test track 40 sector 16
	blk40 := diskReadSector(40, 16)
	if blk40 == nil || blk40[0] != 0x55 || blk40[255] != 0xAA {
		t.Fatalf("diskReadSector(40, 16) = %v, want byte 0=0x55, byte 255=0xAA", blk40)
	}

	// Track 40 sector 17 (out of range, track 40 has 17 sectors 0-16)
	if diskReadSector(40, 17) != nil {
		t.Fatalf("diskReadSector(40, 17) should be nil")
	}

	// Track 41 sector 0 (out of range)
	if diskReadSector(41, 0) != nil {
		t.Fatalf("diskReadSector(41, 0) should be nil")
	}
}

// TestReadDiskOrPRG40Track checks that ReadDiskOrPRG accepts both 35-track
// and 40-track D64 images from disk.
func TestReadDiskOrPRG40Track(t *testing.T) {
	saveMachine(t)

	dir := t.TempDir()

	// Write a dummy 40-track D64 image
	path40 := filepath.Join(dir, "test40.d64")
	if err := os.WriteFile(path40, make([]byte, D64Size40), 0o644); err != nil {
		t.Fatalf("failed to write dummy 40-track file: %v", err)
	}

	img, err := ReadDiskOrPRG(path40)
	if err != nil {
		t.Fatalf("ReadDiskOrPRG(%q) error = %v", path40, err)
	}
	if len(img) != D64Size40 {
		t.Fatalf("len(img) = %d, want %d", len(img), D64Size40)
	}

	// Write an invalid-sized file (e.g. 180000 bytes)
	pathInvalid := filepath.Join(dir, "bad.d64")
	if err := os.WriteFile(pathInvalid, make([]byte, 180000), 0o644); err != nil {
		t.Fatalf("failed to write invalid file: %v", err)
	}
	if _, err := ReadDiskOrPRG(pathInvalid); err == nil {
		t.Fatalf("ReadDiskOrPRG(%q) should have failed for 180000 byte image", pathInvalid)
	}
}

// TestDriveStepping40Track verifies that the drive head can step up to
// track 40 (half-track 78).
func TestDriveStepping40Track(t *testing.T) {
	saveMachine(t)

	data40 := make([]byte, D64Size40)
	InsertDisk(data40)

	// Step head outward/inward to track 40 (half-track 78)
	driveHalfTrack = 78
	if trk := driveTrack(); trk != 40 {
		t.Fatalf("driveTrack() = %d, want 40 for half-track 78", trk)
	}

	// Move stepper past track 40
	via2StorePRB(0x04 | 1) // motor on, step
	if driveHalfTrack > 80 {
		t.Fatalf("driveHalfTrack = %d, expected upper bound 80", driveHalfTrack)
	}
}

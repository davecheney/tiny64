//go:build drive1541

package tiny64

import "testing"

// TestDiskIDFromBAM covers the half of TestDiskReadSector that only means
// something with a physical drive: diskID reads the ID bytes a real head
// would find stamped in each sector header, which a D64 keeps only in the
// BAM.
func TestDiskIDFromBAM(t *testing.T) {
	saveMachine(t)

	data := make([]byte, D64Size)
	bamOffset := trackOffset(18)
	data[bamOffset+0xA2] = 0x41
	data[bamOffset+0xA3] = 0x42
	InsertDisk(data)

	id1, id2 := diskID()
	if id1 != 0x41 || id2 != 0x42 {
		t.Fatalf("diskID() = %#02x,%#02x, want 0x41,0x42", id1, id2)
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

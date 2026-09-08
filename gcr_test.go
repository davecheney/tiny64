package tiny64

import (
	"math/rand/v2"
	"testing"
)

// gcrDecode4Bytes is gcrDecode5Bytes without the validity result, for the
// round-trip checks below.
func gcrDecode4Bytes(src [5]byte) [4]byte {
	dest, ok := gcrDecode5Bytes(src)
	if !ok {
		panic("gcrDecode5Bytes rejected a code gcrEncode4Bytes produced")
	}
	return dest
}

func TestGCREncode4BytesRoundTrip(t *testing.T) {
	for range 1000 {
		var src [4]byte
		for i := range src {
			src[i] = byte(rand.Uint32())
		}
		encoded := gcrEncode4Bytes(src)
		decoded := gcrDecode4Bytes(encoded)
		if decoded != src {
			t.Fatalf("round-trip mismatch: src=%v encoded=%v decoded=%v", src, encoded, decoded)
		}
	}
}

func TestGCREncodeSectorStructure(t *testing.T) {
	var data [256]byte
	for i := range data {
		data[i] = byte(i)
	}

	const tailGap = 12
	got := gcrEncodeSector(nil, 1, 0, 0x41, 0x42, &data, tailGap)

	wantLen := gcrSyncLen + 5 + 5 + gcrHeaderGap + gcrSyncLen + 5*65 + tailGap
	if len(got) != wantLen {
		t.Fatalf("len = %d, want %d", len(got), wantLen)
	}

	for i := range gcrSyncLen {
		if got[i] != 0xFF {
			t.Fatalf("byte %d = %#02x, want sync 0xff", i, got[i])
		}
	}

	headerStart := gcrSyncLen
	header := gcrDecode4Bytes([5]byte{got[headerStart], got[headerStart+1], got[headerStart+2], got[headerStart+3], got[headerStart+4]})
	if header[0] != 0x08 {
		t.Fatalf("header block ID = %#02x, want 0x08", header[0])
	}
	if header[2] != 0 || header[3] != 1 { // sector, track
		t.Fatalf("header sector/track = %d/%d, want 0/1", header[2], header[3])
	}
	wantChecksum := header[2] ^ header[3] ^ 0x42 ^ 0x41
	if header[1] != wantChecksum {
		t.Fatalf("header checksum = %#02x, want %#02x", header[1], wantChecksum)
	}

	dataSyncStart := headerStart + 10 + gcrHeaderGap
	for i := range gcrSyncLen {
		if got[dataSyncStart+i] != 0xFF {
			t.Fatalf("data sync byte %d = %#02x, want 0xff", i, got[dataSyncStart+i])
		}
	}

	dataStart := dataSyncStart + gcrSyncLen
	dataBlock := gcrDecode4Bytes([5]byte{got[dataStart], got[dataStart+1], got[dataStart+2], got[dataStart+3], got[dataStart+4]})
	if dataBlock[0] != 0x07 {
		t.Fatalf("data block ID = %#02x, want 0x07", dataBlock[0])
	}
	if dataBlock[1] != data[0] || dataBlock[2] != data[1] || dataBlock[3] != data[2] {
		t.Fatalf("first data bytes = %v, want %v", dataBlock[1:4], data[0:3])
	}
}

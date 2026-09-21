//go:build !vicmini

package tiny64

import "testing"

// TestSpriteBASlotMask checks the transcription of the sprite BA windows
// from VICE x64sc's 6569 cycle table. Sprite N holds BA low from slot 44+2N
// through 48+2N: two cycles of pointer and data fetch, preceded by three
// cycles of lead time for the CPU to retire in-flight writes.
//
// It lives apart from the rest of ba_test.go because the table it checks
// is the sprite unit's, and a build made with -tags vicmini has no sprite
// unit to hold BA at all.
func TestSpriteBASlotMask(t *testing.T) {
	var want [CyclesPerLine]uint8
	for n := uint16(0); n < 8; n++ {
		for slot := 44 + 2*n; slot <= 48+2*n; slot++ {
			if slot >= CyclesPerLine {
				t.Fatalf("sprite %d BA window runs past end of line at slot %d", n, slot)
			}
			want[slot] |= 1 << n
		}
	}
	if want != spriteBASlotMask {
		t.Errorf("spriteBASlotMask mismatch\n got %v\nwant %v", spriteBASlotMask, want)
	}
}

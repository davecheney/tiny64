//go:build !vicmini

package tiny64

import "testing"

// TestDisplayRunHasNoSpriteBA is what lets phi0lowDisplay drop the sprite
// DMA test and with it the slot it needed. The sprite fetch block runs from
// slot 44 to the end of the line and feeds the next line's display, so no
// slot below that can have a sprite pulling BA low.
//
// It lives apart from the rest of vic_phi0low_test.go because the table it
// reads is the sprite unit's. A build made with -tags vicmini has no
// sprite unit, so phi0lowDisplay's assumption is not something it has to
// assume: nothing pulls BA low anywhere on the line.
func TestDisplayRunHasNoSpriteBA(t *testing.T) {
	for slot := uint16(displayFirstSlot); slot < displaySlotAfter; slot++ {
		if spriteBASlotMask[slot] != 0 {
			t.Errorf("slot %d has sprite BA mask %#02x; phi0lowDisplay assumes none",
				slot, spriteBASlotMask[slot])
		}
	}
}

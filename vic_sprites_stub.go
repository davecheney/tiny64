//go:build vicmini

package tiny64

// This is the sprite seam for a build without the sprite unit. It pairs
// with vic_sprites_full.go, which holds the real thing.
//
// The eight sprites' registers still exist in VICII and still read back
// what was written to them - software is entitled to that, and the
// register file is bytes rather than work. What is gone is everything
// that consumes them: the DMA fetch, the coverage table, the decode
// tables and the compositor.
//
// The tag is for the microcontroller targets. Their panel shows a 320x240
// window of a character-mode screen, so nothing on it is ever covered by
// a sprite, and the unit is instruction stream the board pays for in
// flash and in XIP cache pressure without ever executing.
//
// What this configuration cannot do, stated plainly:
//
//   - The collision registers $D01E and $D01F read 0 for ever, because
//     nothing raises them. A program that waits on a collision waits for
//     ever.
//   - Sprite DMA no longer steals cycles from the CPU, so a program timed
//     against that stealing runs early.
//
// Neither can happen on a screen with no sprites on it, which is the only
// screen this tag is for.
//
// The four painting entry points below are not reached at all: with
// spriteFreeGroup answering true the dot path always takes its plain arm.
// They answer correctly rather than panicking anyway, so that a mistake
// upstream shows up as a sprite that is not drawn rather than as a board
// that stops.

// spriteFreeGroup reports whether no sprite covers any of a group's four
// dots. Nothing is displayed, so nothing covers anything.
func (v *VICII) spriteFreeGroup(dot uint16) bool { return true }

// spriteSoloGroup reports the one sprite covering all four of a group's
// dots. There is never one.
func (v *VICII) spriteSoloGroup(dot uint16) (uint8, bool) { return 0, false }

// graphicsPixelSolo4 paints the four dots of a group one sprite covers.
// Unreachable: spriteFreeGroup answered true. What it would paint with no
// sprite over it is what the plain path paints.
func (v *VICII) graphicsPixelSolo4(dot, reloadOffset uint16, i uint8) (byte, byte, byte, byte) {
	return v.graphicsPixelPlain4(reloadOffset)
}

// graphicsPixel decides one dot through the compositor. Unreachable, for
// the same reason, and the same answer: the dot with nothing over it.
func (v *VICII) graphicsPixel(dot uint16) byte { return v.graphicsPixelPlain() }

// spriteDMAStall reports whether sprite DMA holds the CPU off the bus in
// this slot. No DMA, so BA is the bad-line question alone.
func (v *VICII) spriteDMAStall(slot uint16) bool { return false }

// latchSpriteDisplay advances each sprite's DMA state one raster line.
func (v *VICII) latchSpriteDisplay() {}

// latchSpriteShape fetches one sprite's three shape bytes for the line.
func (v *VICII) latchSpriteShape(i uint8) {}

// rebuildSpriteCoverage works out which dots each displayed sprite covers.
func (v *VICII) rebuildSpriteCoverage() {}

// decodeSpriteRows turns each fetched shape row into the dots it paints.
func (v *VICII) decodeSpriteRows() {}

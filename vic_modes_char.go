//go:build vicmini

package tiny64

// This is the graphics-mode seam for a build that knows one mode. It
// pairs with vic_modes_full.go, which holds all five.
//
// Standard character mode is what the board targets display, and the
// whole of what they display. The other four modes - multicolour text,
// the two bitmap modes and extended background colour - are reachable
// only by writing BMM, ECM or MCM, which nothing on a text screen does,
// so the decode that chooses between them is work done once per sequencer
// reload to arrive at the same answer every time.
//
// What that costs in a full build is not the switch itself so much as
// what the switch keeps alive: refreshGraphicsPalette's two arms, the
// multicolour half of expandGraphicsData, and the three g-access address
// forms.
//
// What this configuration cannot do, stated plainly: a program that sets
// BMM, ECM or MCM gets standard character mode anyway, rather than the
// bitmap or multicolour display a real VIC-II would give it. The
// registers still store what was written, so a program that reads them
// back sees its own value; only the picture ignores it.

// gAccessAddress works out where this cycle's g-access reads from. Text
// mode takes all eight bits of the character pointer, which is the only
// form this build has.
func (v *VICII) gAccessAddress(cb uint16) uint16 {
	return cb<<11 | (v.videoBufferPending&0xFF)<<3 | uint16(v.RC)
}

// loadGraphicsData takes up the g-access byte the sequencer reloads on,
// and settles what the next eight dots paint.
//
// The mode latch and the multicolour test that a full build does here are
// both answered by the tag: v.graphicsMode stays at the modeStandardText
// that Reset left in it, and v.multicolor stays false.
func (v *VICII) loadGraphicsData() {
	v.videoBuffer = v.videoBufferPending
	v.gdSequencer = expandGraphicsData(v.gdPending, false)
	v.refreshGraphicsPalette()
}

// expandGraphicsData widens a g-access byte into the form the dot path
// shifts out: two bits per dot, leftmost dot in the high bits.
//
// The multicolor argument is kept so the call sites and the tests read
// the same in both builds, and is always false here: standard character
// mode shifts eight one-bit pixels, and this build has no mode that does
// anything else.
func expandGraphicsData(data uint8, multicolor bool) uint16 {
	var out uint16
	for i := range 8 {
		if data&(0x80>>i) != 0 {
			out |= 1 << (14 - 2*i)
		}
	}
	return out
}

// refreshGraphicsPalette settles the two colours the next eight dots
// choose between, which in character mode are the background and the
// character's own colour from the video matrix.
//
// A full build picks between those, ECM's four backgrounds, the two
// bitmap forms and the multicolour pairs, and has to say what an invalid
// mode paints. Here there is one answer, and gdForeground is the constant
// that goes with it: index 1 is the foreground, index 0 is not.
func (v *VICII) refreshGraphicsPalette() {
	v.gdColor[0] = v.background[0]
	v.gdColor[1] = byte(v.videoBuffer>>8) & 0x0F
	v.gdForeground = 1 << 1
}

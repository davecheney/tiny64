//go:build !vicmini

package tiny64

// This is the graphics-mode seam for a full build: all five of the
// VIC-II's display modes, and the invalid ones. It pairs with
// vic_modes_char.go, which answers the same calls for a build made with
// -tags vicmini and knows only standard character mode.
//
// What is here is every place the mode is consulted. The per-dot path is
// deliberately not one of them: expandGraphicsData bakes the pixel width
// into the sequencer and refreshGraphicsPalette settles the colours, both
// once per reload, so nextGraphicsColor shifts and indexes without ever
// asking what mode it is in.

// gAccessAddress works out where this cycle's g-access reads from. Which
// form the address takes is the mode's business: the bitmap modes index
// the bitmap by VC, ECM takes only six bits of the character pointer
// because the other two chose a background colour, and the text modes
// take all eight.
func (v *VICII) gAccessAddress(cb uint16) uint16 {
	switch v.selectedGraphicsMode() {
	case modeStandardBitmap, modeMulticolorBitmap:
		return (cb&0x04)<<11 | v.VC<<3 | uint16(v.RC)
	case modeECMText:
		return cb<<11 | (v.videoBufferPending&0x3F)<<3 | uint16(v.RC)
	default:
		return cb<<11 | (v.videoBufferPending&0xFF)<<3 | uint16(v.RC)
	}
}

func (v *VICII) selectedGraphicsMode() uint8 {
	return (v.control1>>4)&0x06 | (v.control2 >> 4 & 0x01)
}

func (v *VICII) loadGraphicsData() {
	v.videoBuffer = v.videoBufferPending
	v.graphicsMode = v.selectedGraphicsMode()
	v.multicolor = v.graphicsMode == modeMulticolorBitmap ||
		(v.graphicsMode == modeMulticolorText && v.videoBuffer&0x0800 != 0)
	v.gdSequencer = expandGraphicsData(v.gdPending, v.multicolor)
	v.refreshGraphicsPalette()
}

// expandGraphicsData widens a g-access byte into the form the dot path
// shifts out: two bits per dot, leftmost dot in the high bits.
//
// The standard modes shift out eight one-bit pixels and the multicolor
// ones four two-bit pixels, each two dots wide - but both cover the same
// eight dots and both index the same gdColor/gdForeground palette, so the
// only thing that differed was how the bits were taken. That is what made
// nextGraphicsColor test v.multicolor on every one of the up to 115,020
// dots a frame, an answer that can only change when the sequencer
// reloads: v.multicolor is written nowhere else, and loadGraphicsData
// runs at most once per bus cycle (see reloadDot).
//
// So the mode is asked here instead, and baked into the encoding. A
// standard pixel becomes the two-bit value 0 or 1; a multicolor pair is
// stored twice, once for each of its dots. Both then shift out under one
// uniform rule, and the dot path keeps no pixel-width state of its own -
// the multicolorHalf parity flag it used to read-modify-write per painted
// dot is subsumed by the duplication.
//
// Eight dots consume all 16 bits either way, after which the register is
// zero and shifts out index 0, exactly as the 8-bit register did.
func expandGraphicsData(data uint8, multicolor bool) uint16 {
	x := uint16(data)
	x = (x | x<<4) & 0x0F0F // ----7654----3210
	x = (x | x<<2) & 0x3333 // --76--54--32--10
	if multicolor {
		// Each pair now sits in its own nibble. Copy it up into the
		// other half of that nibble, so the pixel's two dots each shift
		// out the same value.
		return x | x<<2
	}
	x = (x | x<<1) & 0x5555 // -7-6-5-4-3-2-1-0
	return x
}

// refreshGraphicsPalette works out the colours the graphics sequencer can
// emit, and which of them are foreground, from the graphics mode, the
// latched g-access data and the background colour registers.
//
// nextGraphicsColor used to decide this per dot, branching on the mode
// every time, which was around a tenth of a frame spent re-deriving an
// answer that had not changed.
//
// Only two things can change it, so it is rebuilt at exactly those
// points: a sequencer reload, which brings new g-access data and latches
// a new mode (loadGraphicsData), and a CPU write to one of the four
// background colour registers (WriteRegister). Reset establishes it.
//
// Rebuilding once per cycle instead would need no invalidation rule at
// all, and was tried, but it is the wrong trade on the hardware this
// runs on: the Gopher Badge crops the picture to 320x240, so it paints
// under four dots per bus cycle, and a rebuild that costs over a hundred
// instructions cannot pay for itself against four dot decodes.
// TestGraphicsPaletteStaysConsistentAcrossAFrame is what keeps the
// invalidation rule honest instead.
func (v *VICII) refreshGraphicsPalette() {
	if !v.multicolor {
		switch {
		case v.graphicsMode == modeECMText:
			// ECM steals the top two bits of the character pointer to
			// pick one of the four background registers.
			v.gdColor[0] = v.backgroundColor(uint8(v.videoBuffer>>6) & 0x03)
		case v.graphicsMode == modeStandardBitmap:
			v.gdColor[0] = byte(v.videoBuffer) & 0x0F
		case v.graphicsMode > modeECMText:
			v.gdColor[0] = 0 // invalid mode: the display goes black
		default:
			v.gdColor[0] = v.background[0]
		}
		switch v.graphicsMode {
		case modeStandardText, modeMulticolorText, modeECMText:
			v.gdColor[1] = byte(v.videoBuffer>>8) & 0x0F
			v.gdForeground = 1 << 1
		case modeStandardBitmap:
			v.gdColor[1] = byte(v.videoBuffer>>4) & 0x0F
			v.gdForeground = 1 << 1
		default:
			v.gdColor[1] = 0
			v.gdForeground = 0
		}
		return
	}

	switch v.graphicsMode {
	case modeMulticolorText:
		v.gdColor[0] = v.background[0]
		v.gdColor[1] = v.backgroundColor(1)
		v.gdColor[2] = v.backgroundColor(2)
		// Multicolor text takes its foreground from the low three bits
		// of the colour nibble; the fourth selects multicolor itself.
		v.gdColor[3] = byte(v.videoBuffer>>8) & 0x07
		v.gdForeground = 1 << 3
	case modeMulticolorBitmap:
		v.gdColor[0] = v.background[0]
		v.gdColor[1] = byte(v.videoBuffer>>4) & 0x0F
		v.gdColor[2] = byte(v.videoBuffer) & 0x0F
		v.gdColor[3] = byte(v.videoBuffer>>8) & 0x0F
		v.gdForeground = 1<<1 | 1<<2 | 1<<3
	default:
		v.gdColor[0], v.gdColor[1] = 0, 0
		v.gdColor[2], v.gdColor[3] = 0, 0
		v.gdForeground = 0
	}
}

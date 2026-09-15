package tiny64

const (
	CyclesPerLine       = 63 // PAL: 63 CPU cycles per raster line
	DotsPerCycle        = 8  // each CPU cycle (Phi1 + Phi2) spans 8 dots
	DotsPerLine         = CyclesPerLine * DotsPerCycle
	RasterLinesPerFrame = 312 // PAL total raster lines
	CyclesPerFrame      = CyclesPerLine * RasterLinesPerFrame
	DotsPerFrame        = DotsPerLine * RasterLinesPerFrame

	// dot 0 is the leftmost position of a raster line (inside the left
	// overscan, so not necessarily visible on a given TV); the beam moves
	// one dot right per dot clock, and dots 0-404 carry picture. The
	// remaining dots are the horizontal blanking interval, during which
	// the beam retraces to the left and the line counter advances.
	VisibleDotsPerLine = 405

	// PAL 6569 vertical blanking interval: raster lines 300-311 and 0-15,
	// during which the video signal (and thus the raster) is off.
	firstVBlankLine = 300
	lastVBlankLine  = 15

	// The picture occupies raster lines 16-299; writePixelToBuffer is
	// never called outside FirstVisibleLine..FirstVisibleLine+VisibleLines
	// horizontally 0..VisibleDotsPerLine, so a display only needs a buffer
	// that size.
	FirstVisibleLine = lastVBlankLine + 1
	VisibleLines     = firstVBlankLine - FirstVisibleLine

	regControl1    = 0x11 // $D011: RST8/ECM/BMM/DEN/RSEL/YSCROLL
	regControl2    = 0x16 // $D016: -/-/RES/MCM/CSEL/XSCROLL
	regMemPointers = 0x18 // $D018: VM13-10/CB13-11
	regBorderColor = 0x20 // $D020
	regBackground0 = 0x21 // $D021

	csel = 0x08 // $D016 bit 3: Column Select (CSEL)

	modeStandardText     = 0
	modeMulticolorText   = 1
	modeStandardBitmap   = 2
	modeMulticolorBitmap = 3
	modeECMText          = 4

	// Bad Line Condition raster range (section 3.5 of the VIC Article).
	badLineRasterStart = 0x30
	badLineRasterEnd   = 0xF7

	leftComp38  = 55  // CSEL=0: 38 columns (article $1F)
	leftComp40  = 48  // CSEL=1: 40 columns (article $18)
	rightComp38 = 408 // CSEL=0: 38 columns (article $14F, after X counter wrap)
	rightComp40 = 416 // CSEL=1: 40 columns (article $158, after X counter wrap)

	rightEdge38 = 359
	rightEdge40 = 368
)

// Border unit comparison values, indexed by the RSEL/CSEL control bits.
// Border/display window sizes can be switched mid-frame on real hardware.
// The X values are the VIC Article's section 3.9 coordinates rebased onto
// dot (its X=480 is our dot 0), so they can be compared against dot with
// no conversion; the Y values are raster lines and need none.
var (
	topComp    = [2]uint16{0x37, 0x33}
	bottomComp = [2]uint16{0xF7, 0xFB}
)

type VICII struct {
	dot        uint16 // 0 to 503
	rasterLine uint16 // 0 to 311

	// Keep per-dot and per-cycle scalar state before the larger buffers so
	// TinyGo can use compact fixed-offset accesses.
	//
	// lineVisible caches "rasterLine is outside vblank" for the current
	// raster line. lineDrawable further narrows that to the lines the
	// active pixel sink actually stores. Vertical blanking and the render
	// window are properties of the line, not of the dot, and rasterLine is
	// only ever written by dotclock7's line wrap, so every dot on a line
	// gives the same answer. Caching turns each dotclock's test into a
	// single byte load instead of reloading rasterLine and redoing range
	// compares - which LLVM cannot hoist for us, since the pixel sink call
	// may alias this struct and forces a reload after every paint. Kept in
	// sync by syncLineVisibility.
	lineVisible     bool
	lineDrawable    bool
	mainBorder      bool
	verticalBorder  bool
	rightBorderAt   uint16
	rightBorderOpen bool
	rightBorder     [VisibleDotsPerLine - rightEdge38]uint8
	gdSequencer     uint8
	graphicsMode    uint8
	multicolor      bool
	multicolorHalf  bool

	// gdColor holds the colours the sequencer can emit for each value it
	// shifts out - two of them in the standard modes, four in the
	// multicolor ones - and gdForeground marks which of those count as
	// foreground, the bit sprite collisions and priority are decided on.
	// refreshGraphicsPalette fills both.
	gdColor      [4]uint8
	gdForeground uint8
	borderColor  uint8
	background0  uint8
	control1     uint8
	control2     uint8
	memPointers  uint8

	// Signals driven by the VIC-II and sensed by the CPU
	BA  bool // Bus Available (true = high/free, false = low/stalled)
	AEC bool // Address Enable Control (true = CPU owns Phi2, false = VIC owns Phi2)

	// badLine/allowBadLine/denLatch implement the Bad Line Condition
	// (section 3.5): allowBadLine is latched from DEN once per frame during
	// raster line $30, and badLine is re-evaluated every cycle.
	badLine      bool
	allowBadLine bool
	denLatch     bool

	// idle is the video logic's idle/display state (section 3.7.1): true in
	// idle state (only g-accesses occur, VC/VMLI don't advance), false in
	// display state (c- and g-accesses take place, VC/VMLI advance). Starts
	// true after a reset; transitions to false as soon as there's a Bad Line
	// Condition, and back to true in cycle 58 if RC=7 and there's no Bad
	// Line Condition.
	idle bool

	// VC/VCBase/VMLI/RC drive the video matrix and character row fetch
	// (section 3.7.2 of the VIC Article).
	VMLI   uint8
	RC     uint8
	VC     uint16
	VCBase uint16

	// videoBuffer holds the c-access result (char code + color) consumed by
	// the most recent g-access. gdPending/videoBufferPending hold a
	// g-access's fetch result until it is committed 4 dots later.
	gdPending          uint8
	videoBuffer        uint16
	videoBufferPending uint16

	// Keep the dynamically indexed row buffer at the cold end so it does
	// not push fixed-offset fields out of cheap reach.
	videoMatrixColor [40]uint16

	// Registers not used by the current hot video path remain grouped by
	// address range. ReadRegister and WriteRegister map around the named
	// registers above.
	registers00To10 [0x11]uint8
	registers12To15 [0x04]uint8
	register17      uint8
	registers19To1F [0x07]uint8
	registers22To2E [0x0D]uint8

	rasterCompare      uint16
	rasterIRQTriggered bool
	interruptStatus    uint8
	interruptEnable    uint8
	IRQ                bool

	spriteSpriteCollision uint8
	spriteDataCollision   uint8

	// spriteDisplay is the set of sprites that will be drawn on the next
	// raster line, and spriteShape their already-fetched pattern bytes.
	// Both are latched during the sprite fetch block at the end of a line;
	// see latchSpriteDisplay and latchSpriteShape.
	spriteDisplay uint8
	spriteExpFF   uint8
	spriteRow     [8]uint8
	spriteShape   [8][3]uint8
}

var vic VICII

func VIC() *VICII {
	return &vic
}

// Dot returns the beam's current horizontal position (0 to 503), for
// debugging/tracing tools outside this package.
func (v *VICII) Dot() uint16 {
	return v.dot
}

// RasterLine returns the beam's current raster line (0 to 311), for
// debugging/tracing tools outside this package.
func (v *VICII) RasterLine() uint16 {
	return v.rasterLine
}

// Reset restores the VIC-II's internal video logic state (beam position,
// border flip-flops, VC/VCBase/VMLI/RC, and idle/display state) to their
// power-on values. Registers ($D000-$D02E) are left untouched: real
// hardware doesn't clear them on RESET, KERNAL's IOINIT does that.
func (v *VICII) Reset() {
	v.dot = 0
	v.rasterLine = 0
	v.BA = true
	v.AEC = true
	// Raster line 0 is inside the upper border, which the border unit only
	// leaves at the top comparison on line $33/$37. Both flip-flops
	// therefore have to start set, or the first frame paints graphics over
	// the whole upper border until that comparison arrives.
	v.mainBorder = true
	v.verticalBorder = true
	v.rightBorderAt = 0
	v.rightBorderOpen = false
	v.gdSequencer = 0
	v.graphicsMode = modeStandardText
	v.multicolor = false
	v.multicolorHalf = false
	v.videoBuffer = 0
	v.gdPending = 0
	v.videoBufferPending = 0
	v.videoMatrixColor = [40]uint16{}
	v.VC = 0
	v.VCBase = 0
	v.VMLI = 0
	v.RC = 0
	v.badLine = false
	v.allowBadLine = false
	v.denLatch = false
	v.idle = true
	v.rasterCompare = 0
	v.rasterIRQTriggered = false
	v.interruptStatus = 0
	v.interruptEnable = 0
	v.IRQ = false
	v.refreshGraphicsPalette()
	v.spriteSpriteCollision = 0
	v.spriteDataCollision = 0
	v.spriteDisplay = 0
	v.spriteExpFF = 0
	v.spriteRow = [8]uint8{}
	v.spriteShape = [8][3]uint8{}
	v.syncLineVisibility()
}

func (v *VICII) checkRasterIRQ() {
	if v.rasterLine == v.rasterCompare && !v.rasterIRQTriggered {
		v.interruptStatus |= 0x01
		v.rasterIRQTriggered = true
		v.updateIRQ()
	}
}

func (v *VICII) updateIRQ() {
	v.IRQ = (v.interruptStatus & v.interruptEnable & 0x0F) != 0
}

// syncLineVisibility recomputes the cached line visibility flags from rasterLine.
// It must be called whenever rasterLine is changed by anything other than
// dotclock7's line wrap, which updates the flags itself.
func (v *VICII) syncLineVisibility() {
	v.lineVisible = v.rasterLine < firstVBlankLine && v.rasterLine > lastVBlankLine
	v.lineDrawable = v.rasterLine >= renderFirstLine && v.rasterLine < renderLineAfter
}

func (v *VICII) WriteRegister(addr uint16, value uint8) {
	reg := addr & 0x3F
	switch {
	case reg <= 0x10:
		v.registers00To10[reg] = value
	case reg == regControl1:
		v.control1 = value
		v.rasterCompare = (v.rasterCompare & 0xFF) | (uint16(value&0x80) << 1)
		v.checkRasterIRQ()
	case reg == 0x12:
		v.rasterCompare = (v.rasterCompare & 0x100) | uint16(value)
		v.checkRasterIRQ()
	case reg < regControl2:
		v.registers12To15[reg-0x12] = value
	case reg == regControl2:
		v.control2 = value
		v.sampleSideBorderAtWrite(value)
		v.sampleGraphicsAtWrite(value)
	case reg == 0x17:
		v.register17 = value
	case reg == regMemPointers:
		v.memPointers = value
	case reg == 0x19:
		v.interruptStatus &^= (value & 0x0F)
		v.updateIRQ()
	case reg == 0x1A:
		v.interruptEnable = value & 0x0F
		v.updateIRQ()
	case reg == 0x1E, reg == 0x1F:
		// Collision registers are read-cleared and ignore writes.
	case reg < regBorderColor:
		v.registers19To1F[reg-0x19] = value
	case reg == regBorderColor:
		v.borderColor = value
		v.sampleBorderColorAtWrite()
	case reg == regBackground0:
		v.background0 = value
		v.refreshGraphicsPalette()
	case reg < 0x2F:
		v.registers22To2E[reg-0x22] = value
		if reg <= 0x24 {
			// $D022-$D024 are the other three background colours; the
			// rest of this range is sprite colours, which the graphics
			// sequencer does not read.
			v.refreshGraphicsPalette()
		}
	}
}

func (v *VICII) backgroundColor(index uint8) uint8 {
	switch index {
	case 0:
		return v.background0
	case 1:
		return v.registers22To2E[0]
	case 2:
		return v.registers22To2E[1]
	case 3:
		return v.registers22To2E[2]
	default:
		return 0
	}
}

func (v *VICII) sampleBorderColorAtWrite() {
	if v.dot < rightEdge40 || v.dot >= VisibleDotsPerLine {
		return
	}
	first := v.dot - DotsPerCycle
	if first < rightEdge38 {
		first = rightEdge38
	}
	for dot := first; dot <= v.dot; dot++ {
		v.rightBorder[dot-rightEdge38] = v.borderColor & 0x0F
	}
}

func (v *VICII) selectedGraphicsMode() uint8 {
	return (v.control1>>4)&0x06 | (v.control2 >> 4 & 0x01)
}

func (v *VICII) sampleSideBorderAtWrite(control2 uint8) {
	csel := control2 & csel
	left := (v.dot == leftComp38 && csel == 0) ||
		(v.dot == leftComp40 && csel != 0)
	if left && !v.verticalBorder {
		v.mainBorder = false
	}
	switch v.dot {
	case rightEdge38:
		if csel == 0 {
			v.rightBorderAt = rightEdge38
		} else if v.rightBorderAt == rightEdge38 {
			v.rightBorderAt = 0
		}
	case rightEdge40:
		if csel != 0 {
			v.rightBorderAt = rightEdge40
		} else if v.rightBorderAt == rightEdge40 {
			v.rightBorderAt = 0
		}
	}
	switch v.dot {
	case rightComp38:
		if v.rightBorderAt == rightEdge38 {
			if csel == 0 {
				v.mainBorder = true
			} else {
				v.mainBorder = false
				v.rightBorderAt = 0
				v.rightBorderOpen = true
			}
		}
	case rightComp40:
		if v.rightBorderAt == rightEdge40 {
			if csel != 0 {
				v.mainBorder = true
			} else {
				v.mainBorder = false
				v.rightBorderAt = 0
				v.rightBorderOpen = true
			}
		} else if csel == 0 {
			v.rightBorderOpen = true
		}
	}
}

func (v *VICII) finishSideBorder() {
	if !v.lineDrawable {
		v.rightBorderAt = 0
		v.rightBorderOpen = false
		return
	}
	if v.rightBorderAt != 0 {
		for dot := v.rightBorderAt; dot < VisibleDotsPerLine; dot++ {
			writePixelToBuffer(dot, v.rasterLine, v.rightBorder[dot-rightEdge38])
		}
	} else if !v.rightBorderOpen {
		// Preserve the VIC's boundary pixel when a visible CSEL trick opens
		// the rest of the right border. A later hblank write can open it too.
		writePixelToBuffer(rightEdge40, v.rasterLine, v.rightBorder[rightEdge40-rightEdge38])
	}
	v.rightBorderAt = 0
	v.rightBorderOpen = false
}

func (v *VICII) sampleGraphicsAtWrite(control2 uint8) {
	slot := v.dot / 8
	if slot >= 6 && slot <= 45 && v.dot&0x07 == graphicsReloadPhase(control2) {
		v.loadGraphicsData()
	}
}

func graphicsReloadPhase(control2 uint8) uint16 {
	scroll := uint16(control2 & 0x07)
	// Multicolor pixels span two dot clocks. The final odd scroll position
	// completes its pair at phase 7 and reloads on the following boundary.
	if control2&0x10 != 0 && scroll == 7 {
		return 0
	}
	return scroll
}

func (v *VICII) loadGraphicsData() {
	v.gdSequencer = v.gdPending
	v.videoBuffer = v.videoBufferPending
	v.graphicsMode = v.selectedGraphicsMode()
	v.multicolor = v.graphicsMode == modeMulticolorBitmap ||
		(v.graphicsMode == modeMulticolorText && v.videoBuffer&0x0800 != 0)
	v.multicolorHalf = false
	v.refreshGraphicsPalette()
}

// noReloadDot is a dot no cycle can advance onto, marking a cycle in
// which the graphics sequencer does not reload.
const noReloadDot = 0xFFFF

// reloadDot answers, once for the whole cycle about to be stepped, which
// dot the graphics sequencer reloads on - or noReloadDot, if it does not.
// The sequencer takes up a g-access result XSCROLL dots into each
// character cell, and a g-access completes four dots before the
// unscrolled cell boundary.
//
// A cycle advances the beam onto each of the eight dot phases exactly
// once, so at most one of its dots can be the reload dot, and which one
// cannot change while the cycle runs: control2 reaches the VIC only
// through a CPU store, which completes in TickPhi2 after all eight dots.
//
// The eight dotclocks used to ask this individually, once per dot,
// through a function call that seven of them could never act on. Asking
// once per cycle instead takes 157,248 calls out of a PAL frame.
func (v *VICII) reloadDot() uint16 {
	// The beam is advanced before the reload is tested, so the phase is
	// matched against dots v.dot+1 through v.dot+DotsPerCycle. Exactly
	// one of those is congruent to the reload phase; this is it.
	dot := v.dot + 1 + ((graphicsReloadPhase(v.control2) - v.dot - 1) & 7)
	if slot := dot / DotsPerCycle; slot < 6 || slot > 45 {
		return noReloadDot
	}
	return dot
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
			v.gdColor[0] = v.background0
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
		v.gdColor[0] = v.background0
		v.gdColor[1] = v.backgroundColor(1)
		v.gdColor[2] = v.backgroundColor(2)
		// Multicolor text takes its foreground from the low three bits
		// of the colour nibble; the fourth selects multicolor itself.
		v.gdColor[3] = byte(v.videoBuffer>>8) & 0x07
		v.gdForeground = 1 << 3
	case modeMulticolorBitmap:
		v.gdColor[0] = v.background0
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

// nextGraphicsColor shifts one pixel out of the graphics sequencer and
// reports its colour and whether it is foreground. The shift is the only
// part of this that is genuinely per dot; the colour it lands on was
// decided for the whole cycle by refreshGraphicsPalette.
func (v *VICII) nextGraphicsColor() (byte, bool) {
	var index uint8
	if !v.multicolor {
		index = v.gdSequencer >> 7
		v.gdSequencer <<= 1
	} else {
		// A multicolor pixel is two dots wide, so the pair is only
		// shifted out on the second of them.
		index = v.gdSequencer >> 6
		if v.multicolorHalf {
			v.gdSequencer <<= 2
		}
		v.multicolorHalf = !v.multicolorHalf
	}
	return v.gdColor[index], v.gdForeground&(1<<index) != 0
}

// paintGraphicsPixel emits one pixel through the border unit and sprite compositor.
//
// Section 3.9 of the VIC Article is explicit that the *main* border
// flip-flop alone decides whether border colour reaches the screen; the
// vertical border flip-flop is internal state that only feeds rules 4/5
// and 6. Gating output on the vertical flip-flop instead breaks every
// trick that opens a border, and - because it skips nextGraphicsColor -
// stalls the graphics sequencer for the whole of the upper and lower
// border, desynchronising the shift register against the g-accesses.
//
// nextGraphicsColor is therefore called unconditionally: the sequencer
// keeps shifting behind a closed border exactly as the hardware does, and
// only the colour that is written is replaced.
func (v *VICII) paintGraphicsPixel() {
	graphicsColor, isForeground := v.nextGraphicsColor()
	display := v.spriteDisplay
	if v.dot >= rightEdge38 {
		v.rightBorder[v.dot-rightEdge38] = v.borderColor & 0x0F
	}

	if display == 0 {
		if v.mainBorder {
			graphicsColor = v.borderColor
		}
		writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
		return
	}

	d := v.dot
	r := v.rasterLine
	expandXReg := v.registers19To1F[4]    // $D01D
	multicolorReg := v.registers19To1F[3] // $D01C
	priorityReg := v.registers19To1F[2]   // $D01B
	msbReg := v.registers00To10[0x10]     // $D010

	var (
		hitCount             int
		currentHitMask       uint8
		topSpriteColor       byte
		topSpritePriorityBit bool
	)

	for i := uint8(0); i < 8; i++ {
		mask := uint8(1 << i)
		if display&mask == 0 {
			continue
		}

		// Horizontal range check
		x := uint16(v.registers00To10[i*2])
		if msbReg&mask != 0 {
			x |= 0x100
		}
		expandX := (expandXReg & mask) != 0
		startDot := (24 + x) % DotsPerLine
		var px uint8
		if !expandX {
			if d < startDot || d >= startDot+24 {
				continue
			}
			px = uint8(d - startDot)
		} else {
			if d < startDot || d >= startDot+48 {
				continue
			}
			px = uint8((d - startDot) / 2)
		}

		shape := &v.spriteShape[i]

		multicolor := (multicolorReg & mask) != 0
		var color byte
		if !multicolor {
			b := shape[px/8]
			if (b>>(7-(px%8)))&1 == 0 {
				continue
			}
			color = v.registers22To2E[5+i] & 0x0F
		} else {
			pairIdx := px / 2
			shift := (3 - (pairIdx % 4)) * 2
			pairVal := (shape[pairIdx/4] >> shift) & 0x03
			switch pairVal {
			case 0:
				continue
			case 1:
				color = v.registers22To2E[3] & 0x0F // $D025 extra color 0
			case 2:
				color = v.registers22To2E[5+i] & 0x0F // $D027+i individual color
			case 3:
				color = v.registers22To2E[4] & 0x0F // $D026 extra color 1
			}
		}

		hitCount++
		currentHitMask |= mask
		if hitCount == 1 {
			topSpriteColor = color
			topSpritePriorityBit = (priorityReg & mask) != 0 // $D01B priority
		}
	}

	if hitCount > 1 {
		newCollisions := currentHitMask &^ v.spriteSpriteCollision
		v.spriteSpriteCollision |= currentHitMask
		if newCollisions != 0 {
			v.interruptStatus |= 0x04
			v.updateIRQ()
		}
	}

	if hitCount > 0 && isForeground {
		newCollisions := currentHitMask &^ v.spriteDataCollision
		v.spriteDataCollision |= currentHitMask
		if newCollisions != 0 {
			v.interruptStatus |= 0x08
			v.updateIRQ()
		}
	}

	finalColor := graphicsColor
	// The right edge comparator is sampled on the character-cell boundary,
	// but a mid-line CSEL change can leave mainBorder open for that sample.
	// The boundary pixel is nevertheless part of the right border; masking it
	// here prevents the last graphics pixel from leaking into the border.
	if v.mainBorder {
		finalColor = v.borderColor
	} else if hitCount > 0 {
		if !topSpritePriorityBit || !isForeground {
			finalColor = topSpriteColor
		}
	}

	writePixelToBuffer(d, r, finalColor&0x0F)
}

// ReadRegister reads a VIC-II register, mirrored every 64 bytes across
// $D000-$D3FF. Registers beyond $D02E don't exist and read as all 1 bits.
// $D012 (and bit 7 of $D011) are read-only mirrors of the live raster
// line, distinct from the writable raster IRQ compare latch.
func (v *VICII) ReadRegister(addr uint16) uint8 {
	reg := addr & 0x3F
	switch reg {
	case 0x12:
		return uint8(v.rasterLine)
	case 0x11:
		val := v.control1 & 0x7F
		if v.rasterLine&0x100 != 0 {
			val |= 0x80
		}
		return val
	case 0x19:
		val := 0x70 | (v.interruptStatus & 0x0F)
		if v.IRQ {
			val |= 0x80
		}
		return val
	case 0x1A:
		return 0xF0 | (v.interruptEnable & 0x0F)
	case regControl2:
		return v.control2
	case regMemPointers:
		return v.memPointers
	case regBorderColor:
		return v.borderColor
	case regBackground0:
		return v.background0
	case 0x1E:
		val := v.spriteSpriteCollision
		v.spriteSpriteCollision = 0
		return val
	case 0x1F:
		val := v.spriteDataCollision
		v.spriteDataCollision = 0
		return val
	}
	switch {
	case reg < 0x11:
		return v.registers00To10[reg]
	case reg < regControl2:
		return v.registers12To15[reg-0x12]
	case reg == 0x17:
		return v.register17
	case reg < regBorderColor:
		return v.registers19To1F[reg-0x19]
	case reg < 0x2F:
		return v.registers22To2E[reg-0x22]
	}
	return 0xFF
}

// StepFrame advances the VIC-II, and therefore the rest of the machine it
// clocks, by exactly one PAL frame: CyclesPerFrame bus cycles, each of
// which is stepCycle's DotsPerCycle dots. This is a relative step: it
// lands one frame later at whatever dot and raster position it started
// from, rather than synchronizing to the next frame boundary.
//
// Reset leaves the beam on a bus-cycle boundary and stepCycle keeps it
// there, so that position is always a multiple of DotsPerCycle unless a
// caller has assigned to dot itself.
//
// Earlier attempts at this loop regressed on the Gopher Badge and are
// recorded in stepCycle's comment; this shape (a single flat loop calling
// one function per dot, each with exactly one call site) is the one that
// held up.
func (v *VICII) StepFrame() {
	for range CyclesPerFrame {
		v.stepCycle()
	}
}

// stepCycle advances the beam by exactly one bus cycle: DotsPerCycle
// dots, with the VIC-II's own Phi1 accesses on the 4th and the CPU's Phi2
// on the 8th.
//
// It is the only way the machine advances. The CPU reads and writes VIC
// registers during TickPhi2, after all eight dot phases, and IEC devices
// tick after the CPU. Callers observe the machine at bus-cycle boundaries;
// the individual dot phases remain internal.
//
// Two earlier attempts at a per-cycle helper regressed on the Gopher
// Badge, both while calling the single shared dotclock 8 times:
//   - a nested "for range DotsPerCycle { v.StepDot() }" loop measured
//     ~146.0ms/frame (vs a 143.3ms baseline) even though every function
//     body was inlined - the inner loop's control survived as a real
//     loop re-entered CyclesPerFrame times.
//   - 8 explicit v.StepDot() calls (no inner loop) measured ~161.0ms/
//     frame: 8 call sites pushed StepDot itself past TinyGo's inline
//     threshold, turning each call into a real BL/BX into a 352-byte
//     function.
//
// Both regressions trace back to repeatedly entering that general per-dot
// path. This version instead gives each of the 8 dots in a cycle its own
// function - dotclock0 through dotclock7 - so that each has exactly one
// call site. That is the whole trick: LLVM inlines an internal function
// with a single call site near-unconditionally, because the original body
// is deleted afterwards and net code size barely moves, whereas N call
// sites into one shared function mean N copies and the cost threshold
// refuses all of them. Verified in the emitted IR: with the split, no
// dotclock survives as a function and stepCycle contains zero calls into
// one; collapsing the identical bodies back into a single function makes
// it reappear with a real call per dot.
//
// Splitting them apart also means each only contains the checks that dot's
// position can actually reach: the border comparisons and the line/frame-wrap
// and g-access-commit logic only ever trigger on specific dots within a
// cycle (see each function's comment), so the six interior dots' bodies are
// smaller besides.
func (v *VICII) stepCycle() {
	// Two answers the whole cycle shares, established before any dot
	// moves. Vertical blanking is a property of the raster line, not of
	// the dot, so it is the same for all eight; lineVisible caches it,
	// and dotclock0 through dotclock6 therefore carry no vblank check of
	// their own. The graphics sequencer's reload dot is fixed for the
	// cycle for the reason reloadDot gives, so the dotclocks are handed
	// it rather than each working it out.
	reload := v.reloadDot()
	if v.lineDrawable {
		v.dotclock0(reload)
		v.dotclock1(reload)
		v.dotclock2(reload)
		v.dotclock3(reload)
	} else {
		v.dot += 4
	}
	v.phi0low()
	if v.lineDrawable {
		v.dotclock4(reload)
		v.dotclock5(reload)
		v.dotclock6(reload)
	} else if v.lineVisible {
		v.dot += 2
		v.dotclock6(reload)
	} else {
		v.dot += 3
	}
	// dotclock7 always runs, on blanked lines too: it owns the line wrap,
	// and the dot it paints is the first of the new line, which may have
	// just become visible, so paint above cannot speak for it. Keeping it
	// outside the branch also leaves it, phi0low, phi0high and TickPhi2
	// with exactly one call site each.
	v.dotclock7(reload)
	v.phi0high()
	cpu.TickPhi2()
	iecTick()
}

// StepCycle advances the VIC-II, and therefore the rest of the machine it
// clocks, by exactly one bus cycle. It is stepCycle under an exported
// name, for front ends that want to watch the machine a cycle at a time
// rather than a frame at a time; a cycle is the finest grain at which
// there is anything new to see.
//
// The indirection is deliberate. StepFrame's loop calls stepCycle, whose
// inlining into it is load-bearing (see stepCycle), and exporting that
// function outright would put its linkage at the mercy of whether the
// linker can still prove it internal.
func (v *VICII) StepCycle() {
	v.stepCycle()
}

// StepFrame advances the singleton machine by exactly one PAL frame. See
// VICII.StepFrame for where in the frame it starts and stops.
func StepFrame() {
	vic.StepFrame()
}

// dotclock0 through dotclock5 execute the interior phases of a bus cycle.
// Each advances the beam to dots 1 through 6 respectively. Every check
// beyond the hblank test and the pixel paint itself only ever triggers on
// one specific dot within a cycle:
//   - the line/frame wrap only happens advancing off dot 503 (the last
//     dot of a line, DotsPerLine-1), which only the 8th dot of a cycle
//     (dotclock7) can reach, since DotsPerLine is a multiple of
//     DotsPerCycle;
//   - the border comparisons only match dots 48, 55, 408, and 416 (see
//     leftComp/rightComp), all of which are ≡ 7 or 0 (mod 8);
//   - the g-access result reloads on the XSCROLL-selected dot of a
//     character cell, so every phase may need to check that condition.
//
// So these six functions are byte-for-byte identical to each other: just
// the beam advance, the hblank test, and the pixel paint. The vblank test
// is gone; stepCycle establishes that once per cycle for all 8 dots.
//
// Do not deduplicate them into one function called six times. The
// duplication is deliberate, and it is what makes them inline. LLVM
// decides inlining per call site, and an internal function with exactly
// one call site is inlined near-unconditionally: the original body is
// deleted afterwards, so net code size barely moves. Six call sites into
// one shared function lose that, and inlining would instead mean six
// copies of this body, which exceeds the cost threshold - so LLVM declines
// every one of them and stepCycle pays six real calls per bus cycle
// instead of none. That is exactly the ~161.0ms/frame regression recorded
// in stepCycle's comment. Note the trap: the shared version is *smaller*
// in flash precisely because it failed to inline, so code size is not
// evidence that it is faster.
func (v *VICII) dotclock0(reload uint16) {
	if v.dot == rightEdge40 {
		// CPU writes at the boundary occur after its pixel was first
		// generated. Capture the resulting border color on the next dot.
		v.rightBorder[rightEdge40-rightEdge38] = v.borderColor & 0x0F
	}
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// dotclock1 is dotclock0 for cycle phase 1 - see dotclock0's comment.
func (v *VICII) dotclock1(reload uint16) {
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// dotclock2 is dotclock0 for cycle phase 2 - see dotclock0's comment.
func (v *VICII) dotclock2(reload uint16) {
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// dotclock3 is dotclock0 for cycle phase 3 - see dotclock0's
// comment. phi0low runs immediately after this call (see stepCycle).
func (v *VICII) dotclock3(reload uint16) {
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// dotclock4 is dotclock0 for cycle phase 4 - see dotclock0's comment.
func (v *VICII) dotclock4(reload uint16) {
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// dotclock5 is dotclock0 for cycle phase 5 - see dotclock0's comment.
func (v *VICII) dotclock5(reload uint16) {
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// dotclock6 handles cycle phase 6. Its beam advance reaches phase 6
// (8N+7), where the 38-column left border comparison occurs.
func (v *VICII) dotclock6(reload uint16) {
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}

	if v.dot == rightEdge38 && v.control2&csel == 0 {
		v.rightBorderAt = rightEdge38
	}
	if v.dot == leftComp38 && v.control2&csel == 0 {
		rsel := (v.control1 >> 3) & 1
		if v.rasterLine == bottomComp[rsel] {
			v.verticalBorder = true
		}
		if v.rasterLine == topComp[rsel] && v.control1&0x10 != 0 {
			v.verticalBorder = false
		}
		if !v.verticalBorder {
			v.mainBorder = false
		}
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// dotclock7 handles cycle phase 7. Its beam advance reaches phase 7
// (8N+8 = 0 mod 8), where line wrap, the 40-column left comparison, and
// both right comparisons occur. The VIC X counter wraps before the right
// comparisons, placing article coordinates $14f/$158 at dots 408/416.
func (v *VICII) dotclock7(reload uint16) {
	v.dot++
	if v.dot == reload {
		v.loadGraphicsData()
	}
	if v.dot >= DotsPerLine {
		v.finishSideBorder()
		v.dot = 0
		v.rasterLine++
		if v.rasterLine >= RasterLinesPerFrame {
			v.rasterLine = 0
		}
		v.rasterIRQTriggered = false
		// The only place rasterLine changes in the hot path, so the only
		// place the cached visibility answers can go stale.
		v.lineVisible = v.rasterLine < firstVBlankLine && v.rasterLine > lastVBlankLine
		v.lineDrawable = v.rasterLine >= renderFirstLine && v.rasterLine < renderLineAfter
		if v.rasterLine != 0 {
			v.checkRasterIRQ()
		}
	} else if v.rasterLine == 0 && v.dot == DotsPerCycle {
		// Raster line 0 is compared in cycle 2; all other lines are
		// compared in cycle 1.
		v.checkRasterIRQ()
	}

	if !v.lineVisible {
		return
	}

	if v.dot == rightEdge40 && v.control2&csel != 0 {
		v.rightBorderAt = rightEdge40
	}
	if v.dot == rightComp38 && v.rightBorderAt == rightEdge38 {
		if v.control2&csel == 0 {
			v.mainBorder = true
		} else {
			v.mainBorder = false
			v.rightBorderAt = 0
		}
	}
	if v.dot == rightComp40 && v.rightBorderAt == rightEdge40 {
		if v.control2&csel != 0 {
			v.mainBorder = true
		} else {
			v.mainBorder = false
			v.rightBorderAt = 0
		}
	}
	if v.dot == leftComp40 && v.control2&csel != 0 {
		rsel := (v.control1 >> 3) & 1
		if v.rasterLine == bottomComp[rsel] {
			v.verticalBorder = true
		}
		if v.rasterLine == topComp[rsel] && v.control1&0x10 != 0 {
			v.verticalBorder = false
		}
		if !v.verticalBorder {
			v.mainBorder = false
		}
	}

	if v.dot >= VisibleDotsPerLine {
		return
	}

	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	v.paintGraphicsPixel()
}

// phi0low runs on the first dot of every 8-dot cycle: while the VIC-II is
// in charge of the bus, it performs its own memory reads here (section
// 3.7.2 of the VIC Article). Sprites are not implemented yet.
//
// cycleRaster0/cycleRaster30/cycleIsBadLine/cycleIsCAccess are inlined
// directly here (each had exactly one call site, unconditional or nearly
// so) to remove function-call overhead from the frame fast path.
func (v *VICII) phi0low() {
	// slot indexes the 8-dot bus cycles across a line. The article's cycle
	// numbering starts 10 slots later (its cycle N is our slot N-11, mod
	// 63), so the constants below are the article's rebased onto slot.
	slot := v.dot / 8

	// cycleRaster0 (article cycle 1): resets VCBase at the start of raster
	// line 0.
	if v.rasterLine == 0 && slot == 53 {
		v.VCBase = 0
	}

	// cycleRaster30: latches whether DEN was set at any point during
	// raster line $30 into allowBadLine, at the end of that line
	// (section 3.5).
	if v.rasterLine == badLineRasterStart {
		if v.control1&0x10 != 0 {
			v.denLatch = true
		}
		if slot == 52 { // article cycle 63, the line's last
			v.allowBadLine = v.denLatch
			v.denLatch = false
		}
	}

	// cycleIsBadLine: evaluates the Bad Line Condition (section 3.5):
	// raster within $30-$F7, its lower 3 bits matching YSCROLL, and DEN
	// having been set at some point during raster line $30.
	yscroll := v.control1 & 0x07
	badLine := v.rasterLine >= badLineRasterStart && v.rasterLine <= badLineRasterEnd &&
		uint8(v.rasterLine)&0x07 == yscroll && v.allowBadLine
	v.badLine = badLine
	// "The transition from idle to display state occurs as soon as there
	// is a Bad Line Condition" (section 3.7.1).
	if badLine {
		v.idle = false
	}

	switch slot {
	case 1, 2, 3: // article cycles 12-14
		v.cycleSetVicCounter()
	case 47: // article cycle 58
		v.cycleGotoIdle()
	case 52: // article cycle 63
		v.cycleBorderComp()
	}

	// cycleIsCAccess: pulls BA low for the duration of a Bad Line's
	// c-accesses, article cycles 12-54. Note the three cycle lead: the
	// first c-access is at article cycle 15 (slot 4).
	//
	// Sprite DMA (section 3.6.3) additionally pulls BA low for five
	// cycles per DMA-active sprite - two fetch cycles plus the same three
	// cycle lead - in the fixed windows given by spriteBASlotMask.
	//
	// The sprite fetch block runs from slot 44 to the end of the line and
	// feeds the next line's display window, so the display decision is
	// latched here, before BA consults it, and each sprite's shape is
	// fetched in its own window.
	if slot == 44 {
		v.latchSpriteDisplay()
	}
	if slot >= 47 && slot&1 == 1 {
		v.latchSpriteShape(uint8((slot - 47) / 2))
	}
	v.BA = !(slot >= 1 && slot <= 43 && badLine) && !v.spriteDMAStall(slot)

	if slot >= 5 && slot <= 44 {
		v.cycleGAccess()
	}
}

// spriteBASlotMask gives, for each bus cycle of a line, the set of sprites
// whose BA window covers it. Sprite N's BA runs from slot 44+2N through
// 48+2N inclusive: the two cycles of pointer/data fetch it actually needs
// (slots 47+2N and 48+2N), preceded by the three cycles of lead time the
// VIC-II gives the CPU to retire any in-flight write cycles before the bus
// is taken away. Transcribed from the 6569 (PAL) cycle table in VICE's
// x64sc (viciisc/vicii-chip-model.c), rebased from its article cycle
// numbering onto slot (article cycle = slot + 11).
var spriteBASlotMask = [CyclesPerLine]uint8{
	44: 0x01, 45: 0x01, 46: 0x03, 47: 0x03, 48: 0x07, 49: 0x06,
	50: 0x0E, 51: 0x0C, 52: 0x1C, 53: 0x18, 54: 0x38, 55: 0x30,
	56: 0x70, 57: 0x60, 58: 0xE0, 59: 0xC0, 60: 0xC0, 61: 0x80,
	62: 0x80,
}

// spriteDMAStall reports whether any sprite whose BA window covers this
// slot is currently DMA active, and so is holding BA low.
func (v *VICII) spriteDMAStall(slot uint16) bool {
	return spriteBASlotMask[slot]&v.spriteDisplay != 0
}

// latchSpriteDisplay advances each sprite's DMA state one raster line, and
// leaves in spriteDisplay the set of sprites that will be drawn on the
// *next* line, with spriteRow holding the row of each sprite's shape to
// fetch.
//
// The VIC-II runs this once per line, in the first phase of article cycle
// 55 (our slot 44). A sprite's DMA is a one-shot trigger: it turns on for
// the single line where the sprite's Y matches the low eight bits of
// RASTER, and from then on the chip walks the sprite's 21 rows off an
// internal counter (MCBASE) with no further reference to Y. Rewriting Y
// part way through cannot retract the rows already committed, which is
// exactly what a sprite multiplexer relies on: it reprograms a sprite for
// its next slot while the current one is still being drawn.
//
// Everything after this point - the pointer and data fetches, and the
// display window on the following line - runs off the state latched here.
func (v *VICII) latchSpriteDisplay() {
	enable := v.registers12To15[3] // $D015
	expandY := v.register17        // $D017
	line := uint8(v.rasterLine)

	for i := uint8(0); i < 8; i++ {
		mask := uint8(1) << i

		// Advance a sprite already under DMA to its next row. Y expansion
		// holds each row for two lines by only advancing on every second
		// one, tracked by the expansion flip flop.
		if v.spriteDisplay&mask != 0 {
			advance := true
			if expandY&mask != 0 {
				v.spriteExpFF ^= mask
				advance = v.spriteExpFF&mask == 0
			}
			if advance {
				if v.spriteRow[i] == 20 {
					v.spriteDisplay &^= mask
				} else {
					v.spriteRow[i]++
				}
			}
		}

		// A sprite whose DMA is off starts a new 21 row run on the line
		// its Y names. The comparison is against the low eight bits of
		// RASTER, so on PAL a sprite positioned above line 56 is triggered
		// a second time when the raster passes 256 + Y.
		if v.spriteDisplay&mask == 0 && enable&mask != 0 &&
			v.registers00To10[i*2+1] == line {
			v.spriteDisplay |= mask
			v.spriteRow[i] = 0
			v.spriteExpFF |= mask
		}
	}
}

// latchSpriteShape performs sprite i's pointer and data fetches, the
// p-access and three s-accesses the VIC-II makes in article cycles 58+2i
// and 59+2i. The three bytes are one row of the sprite, chosen by the row
// counter the chip derives from how far into its Y band the sprite is.
func (v *VICII) latchSpriteShape(i uint8) {
	if v.spriteDisplay&(1<<i) == 0 {
		return
	}
	screenBase := (uint16(v.memPointers) >> 4) & 0x0F << 10
	ptr := plaVICSpriteLoad(screenBase + 0x03F8 + uint16(i))
	addr := uint16(ptr)*64 + uint16(v.spriteRow[i])*3
	v.spriteShape[i][0] = plaVICSpriteLoad(addr)
	v.spriteShape[i][1] = plaVICSpriteLoad(addr + 1)
	v.spriteShape[i][2] = plaVICSpriteLoad(addr + 2)
}

// cycleSetVicCounter loads VC from VCBase and resets VMLI (and RC on a Bad
// Line), on the first phase of cycle 14 (section 3.7.2).
func (v *VICII) cycleSetVicCounter() {
	v.VC = v.VCBase
	v.VMLI = 0
	if v.badLine {
		v.RC = 0
	}
}

// cycleGAccess reads one row of graphics data and advances VC/VMLI
// (section 3.7.2/3.7.3). The fetched
// byte isn't displayed immediately: it's latched in gdPending and
// committed at the XSCROLL-selected dot of the next character cell, at
// least 4 dots after this cycle's own dot&7==4.
func (v *VICII) cycleGAccess() {
	if v.idle {
		v.videoBufferPending = 0
		v.gdPending = plaVICLoad(0x3FFF)
		return
	}

	if v.VMLI < 40 {
		v.videoBufferPending = v.videoMatrixColor[v.VMLI]
	} else {
		v.videoBufferPending = 0
	}

	cb := (uint16(v.memPointers) >> 1) & 0x07
	var addr uint16
	switch v.selectedGraphicsMode() {
	case modeStandardBitmap, modeMulticolorBitmap:
		addr = (cb&0x04)<<11 | v.VC<<3 | uint16(v.RC)
	case modeECMText:
		addr = cb<<11 | (v.videoBufferPending&0x3F)<<3 | uint16(v.RC)
	default:
		addr = cb<<11 | (v.videoBufferPending&0xFF)<<3 | uint16(v.RC)
	}
	v.gdPending = plaVICLoad(addr)

	// VC is ten bits wide (section 3.7.2), so it wraps at 1024 rather
	// than counting on. That is not a guard against a case that cannot
	// happen: section 3.14.4's "Linecrunch" walks VCBASE up by 40 a line
	// by aborting Bad Lines, deliberately, to scroll the screen upwards
	// without moving graphics memory, and the article is explicit that
	// "VCBASE wraps around to zero when reaching 1024". Unmasked, VC
	// carried out of its field into VM in the c-access address, shifted
	// past the top of the bank in the bitmap g-access address, and
	// indexed colour RAM - which is exactly these ten bits wide - out of
	// range.
	v.VC = (v.VC + 1) & 0x3FF
	v.VMLI++
}

// cycleGotoIdle checks for the end of a character row, on the first phase
// of cycle 58 (section 3.7.2, rule 5): if RC=7, the video logic goes to
// idle state and VCBase is loaded from VC; RC is then only incremented if
// still (or again, per section 3.7.3.9's edge case) in display state.
func (v *VICII) cycleGotoIdle() {
	if v.RC == 7 {
		v.idle = true
		v.VCBase = v.VC
	}
	if !v.idle {
		v.RC++
	}
}

// cycleBorderComp sets/resets the vertical border flip-flop from the Y
// coordinate alone, on cycle 63 (rules 2/3 of section 3.9; independent of
// the X-driven rules 4/5/6 already handled by borderUnit).
func (v *VICII) cycleBorderComp() {
	rsel := (v.control1 >> 3) & 1
	if v.rasterLine == bottomComp[rsel] {
		v.verticalBorder = true
	}
	if v.rasterLine == topComp[rsel] && v.control1&0x10 != 0 {
		v.verticalBorder = false
	}
}

// phi0high runs on the 4th dot of every 8-dot cycle: it performs a Bad
// Line's c-access (article cycles 15-54) and hands the bus to the CPU for
// Phi2.
func (v *VICII) phi0high() {
	// slot as in phi0low, but 4 dots later, so its offset from the
	// article's cycle numbering differs by one.
	slot := (v.dot - 1) / 8
	if slot >= 4 && slot <= 43 && v.badLine {
		v.cycleCAccess()
	}

	// AEC mirrors BA with a delay, or is directly controlled here
	v.AEC = v.BA
}

// cycleCAccess reads one character pointer + color entry from the video
// matrix into the current row's buffer, during a Bad Line (section 3.7.2).
func (v *VICII) cycleCAccess() {
	vm := (uint16(v.memPointers) >> 4) & 0x0F
	char := plaVICLoad((vm << 10) + v.VC)
	// Colour RAM is a dedicated 2114 chip wired directly to the VIC-II's
	// colour bus, not part of the 64K address space the c-access above
	// reads through, so it is read here independently of plaVICLoad.
	color := colorRAM[v.VC]
	if v.VMLI < 40 {
		v.videoMatrixColor[v.VMLI] = uint16(color)<<8 | uint16(char)
	}
}

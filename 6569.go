package tiny64

import "math/bits"

const (
	CyclesPerLine       = 63 // PAL: 63 CPU cycles per raster line
	DotsPerCycle        = 8  // each CPU cycle (Phi1 + Phi2) spans 8 dots
	DotsPerLine         = CyclesPerLine * DotsPerCycle
	RasterLinesPerFrame = 312 // PAL total raster lines
	CyclesPerFrame      = CyclesPerLine * RasterLinesPerFrame
	DotsPerFrame        = DotsPerLine * RasterLinesPerFrame

	// dot 0 is the leftmost position of a raster line (inside the left
	// overscan, so not necessarily visible on a given TV); the beam moves
	// one dot right per dot clock, and dots 0-407 carry picture. The
	// remaining dots are the horizontal blanking interval, during which
	// the beam retraces to the left and the line counter advances.
	//
	// 408 is how much of the line VICE keeps with full borders on. The
	// display window lands at dots 48-367 either way, so this only widens
	// the right border; the left edge is already where VICE puts it.
	VisibleDotsPerLine = 408

	// PAL 6569 vertical blanking interval: raster lines 301-311 and 0-7.
	// These bound the picture rather than the 6569's own blanking, and are
	// set to keep the same 293 lines VICE shows with full borders on.
	firstVBlankLine = 301
	lastVBlankLine  = 7

	// The picture occupies raster lines 8-300; writePixels4ToBuffer is
	// never called outside FirstVisibleLine..FirstVisibleLine+VisibleLines
	// horizontally 0..VisibleDotsPerLine, so a display only needs a buffer
	// that size. The display window is raster lines 51-250, leaving 43
	// lines of border above it and 50 below, as VICE has it.
	FirstVisibleLine = lastVBlankLine + 1
	VisibleLines     = firstVBlankLine - FirstVisibleLine

	regControl1    = 0x11 // $D011: RST8/ECM/BMM/DEN/RSEL/YSCROLL
	regControl2    = 0x16 // $D016: -/-/RES/MCM/CSEL/XSCROLL
	regMemPointers = 0x18 // $D018: VM13-10/CB13-11
	regBorderColor = 0x20 // $D020
	regBackground0 = 0x21 // $D021
	regBackground3 = 0x24 // $D024, the last of the four background colours

	csel = 0x08 // $D016 bit 3: Column Select (CSEL)

	modeStandardText     = 0
	modeMulticolorText   = 1
	modeStandardBitmap   = 2
	modeMulticolorBitmap = 3
	modeECMText          = 4

	// Bad Line Condition raster range (section 3.5 of the VIC Article).
	badLineRasterStart = 0x30
	badLineRasterEnd   = 0xF7

	leftComp38 = 55 // CSEL=0: 38 columns (article $1F)
	leftComp40 = 48 // CSEL=1: 40 columns (article $18)
	// The right comparison values, the article's $14F and $158, rebased
	// onto dot by the same mapping that carries the two left ones above.
	rightEdge38 = 359 // CSEL=0: 38 columns (article $14F)
	rightEdge40 = 368 // CSEL=1: 40 columns (article $158)

	// The three bus cycles those four dots fall in. The comparator has to
	// see every dot, but only these slots contain a dot it can match, so
	// stepCycle decides once a slot whether the dot path need ask at all.
	// leftComp40 and leftComp38 share a slot; the right pair does not.
	borderSlotLeft    = leftComp40 / DotsPerCycle
	borderSlotRight38 = rightEdge38 / DotsPerCycle
	borderSlotRight40 = rightEdge40 / DotsPerCycle

	// The render window in slots. Its edges are bus-cycle aligned, so a
	// slot is wholly inside it or wholly outside, and the question can be
	// asked of the slot number rather than of the beam.
	// The bus cycles the graphics sequencer can take up a g-access result
	// in. The g-access runs in slots 5 to 44 and the reload is one slot
	// behind it, so these are that window shifted by one.
	reloadFirstSlot = 6
	reloadSlotAfter = 46

	// The interior of the display window: every slot between the first
	// border comparison and the second, which is the longest run on a line
	// and the one where every question phi0low asks has a fixed answer.
	displayFirstSlot = borderSlotLeft + 1
	displaySlotAfter = borderSlotRight38

	renderFirstSlot = renderFirstDot / DotsPerCycle
	renderSlotAfter = renderDotAfter / DotsPerCycle
	visibleSlots    = VisibleDotsPerLine / DotsPerCycle

	// vincSlot is the bus cycle carrying VINC, the VIC-II's own
	// increment-vertical-counter strobe, and so the bus cycle the raster
	// counter moves in. The VIC-II manual's horizontal decode table puts
	// VINC at X 404-412, which this coordinate system carries to dots
	// 428-436, inside slot 53.
	//
	// VINC is not the line's visible start and not the sync pulse. The
	// manual has HBLANK opening at 396 and HSYNC at 416-452: the counter
	// moves one cycle after video is blanked and twelve dots before sync
	// fires, so the whole of the retrace and back porch that follow belong
	// to the new line. Dot 0 is the leftmost dot the beam paints, 80 dots
	// after VINC, which is why this reads as slot 53 rather than slot 0.
	//
	// Slots 52 and 54 are both ruled out by measurement as well as by the
	// table: at 52 the-passengers matches VICE on 142 of 293 rows against
	// 291 here, and paints 9 boundaries on odd dots, which multicolour
	// cannot emit; at 54 it moves 6 pixels off the reference.
	vincSlot = 53

	// RasterIncrementCycle is the same bus cycle counted the way a trace
	// counts them, from 1 rather than from 0. It is exported because a
	// trace's DOT and RASTER columns do not step together: RASTER moves on
	// VINC, nine cycles before the beam wraps.
	RasterIncrementCycle = vincSlot + 1
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
	// slot is which of the line's CyclesPerLine bus cycles the machine is
	// in. It is the only beam counter kept: a dot position is slot times
	// DotsPerCycle plus an offset the dot path holds in a register, so
	// storing dots would be storing this scaled by eight.
	slot uint16 // 0 to 62

	// bank is the base of the 16K window the VIC-II fetches through,
	// selected by CIA2's port A. It is stored rather than derived because
	// the port moves a handful of times a frame at most, while every g-,
	// c- and sprite access reads through it. setBank is the only writer.
	bank uint16

	// beamLine is the raster line the beam is on, and so which framebuffer
	// row a painted dot lands in. Dot 0 is the leftmost pixel of the line
	// and beamLine steps there, when the beam wraps. That is the whole of
	// the coordinate system: (dot, beamLine) names a pixel.
	//
	// rasterLine is a register, not a position: the value $D012 reads,
	// with $D011's bit 8 folded into it exactly as $D010's ninth bits are
	// folded into spriteX. ReadRegister splits it back out. It steps on
	// VINC, in slot 53, because that is where the VIC-II tells the monitor
	// to recall its beam - an artifact of driving a CRT, not a fact about
	// where the picture starts.
	//
	// Every rule in this file keys off rasterLine rather than beamLine:
	// the Bad Line condition, the border unit's top and bottom
	// comparisons, sprite DMA and the raster IRQ all read the counter the
	// CPU reads, because on the chip there is only one. Programs sync on
	// it and then count cycles, which is what pins it to slot 53.
	//
	// The two differ only from VINC to the end of the line - 80 dots, all
	// blanked - so they always agree wherever a pixel is written.
	// TestBeamLineAgreesWithRasterWherePainted pins that.
	beamLine   uint16 // 0 to 311
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
	lineVisible    bool
	lineDrawable   bool
	mainBorder     bool
	verticalBorder bool
	// gdSequencer is the graphics data shift register, held two bits per
	// dot rather than as the hardware's eight bits so that both pixel
	// widths shift out under one rule - see expandGraphicsData.
	gdSequencer  uint16
	graphicsMode uint8
	multicolor   bool

	// gdColor holds the colours the sequencer can emit for each value it
	// shifts out - two of them in the standard modes, four in the
	// multicolor ones - and gdForeground marks which of those count as
	// foreground, the bit sprite collisions and priority are decided on.
	// refreshGraphicsPalette fills both.
	gdColor      [4]uint8
	gdForeground uint8
	borderColor  uint8

	// background holds $D021-$D024. They are one contiguous, index
	// addressed group in the chip, and ECM reaches indices 1-3 through
	// the top two bits of a character pointer; index 0 is the one every
	// other mode uses.
	background  [4]uint8
	control1    uint8
	control2    uint8
	memPointers uint8

	// Signals driven by the VIC-II and sensed by the CPU
	BA          bool  // Bus Available, wired to CPU RDY (low holds reads, not writes)
	baLowCycles uint8 // Consecutive BA-low cycles so far, this one included.

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

	lightpenX uint8 // $D013
	lightpenY uint8 // $D014

	rasterCompare      uint16
	rasterIRQTriggered bool
	interruptStatus    uint8
	interruptEnable    uint8
	IRQ                bool

	// The sprite registers that are a bit per sprite, kept as the whole
	// bytes the chip presents at these addresses: the CPU reads and writes
	// them that way - LDA $D015 / ORA #1 / STA $D015 is the multiplexer
	// idiom - and the dot path wants one load and a mask test per sprite.
	// spriteBit is the one place that says which bit is whose.
	//
	// spriteDisplay and spriteExpFF are internal sets in the same encoding,
	// kept adjacent so one base register reaches the whole group.
	spriteEnable          uint8 // $D015 MxE
	spriteExpandY         uint8 // $D017 MxYE
	spritePriority        uint8 // $D01B MxDP, set = sprite behind foreground
	spriteMulticolor      uint8 // $D01C MxMC
	spriteExpandX         uint8 // $D01D MxXE
	spriteMC0             uint8 // $D025, multicolor bit pair 01
	spriteMC1             uint8 // $D026, multicolor bit pair 11
	spriteSpriteCollision uint8 // $D01E, read-cleared
	spriteDataCollision   uint8 // $D01F, read-cleared

	// spriteDisplay is the set of sprites that will be drawn on the next
	// raster line, and spriteShape their already-fetched pattern bytes.
	// Both are latched during the sprite fetch block at the end of a line;
	// see latchSpriteDisplay and latchSpriteShape.
	spriteDisplay uint8
	spriteExpFF   uint8

	// The per-sprite values, one array each. spriteX holds the whole nine
	// bit X: $D010 has no storage of its own - it is bit 8 of each sprite's
	// X, gathered on read and scattered on write - so the dot path never
	// has to reassemble it. These are indexed dynamically, so they sit at
	// the cold end for the reason videoMatrixColor does.
	spriteX     [8]uint16 // $D000+2i, with $D010's bit folded in
	spriteY     [8]uint8  // $D001+2i
	spriteColor [8]uint8  // $D027+i, stored whole and masked at use
	spriteRow   [8]uint8
	spriteShape [8][3]uint8

	// spritePixels holds each sprite's row already decoded to one entry
	// per dot it covers, so the dot path indexes it rather than pulling
	// bits out of spriteShape once for every dot. decodeSpriteRow fills
	// it; see there for the encoding and for what invalidates it.
	//
	// 48 entries because X expansion doubles a sprite's 24 dots. Only the
	// first 24 are written when it is not expanded.
	spritePixels [8][48]uint8

	// spriteCoverage answers, for one dot, which sprites' display windows
	// cover it, and spriteStart holds the dot each of those windows opens
	// on. rebuildSpriteCoverage fills both; see its comment for why the
	// dot path asks a table rather than eight range tests.
	//
	// The table is 512 entries for a 504 dot line so that the index can be
	// masked rather than checked: a dot is always inside a line, but the
	// compiler cannot know that, and a bounds check on the hottest load in
	// the emulator is exactly the kind of branch this path is won by
	// removing.
	spriteCoverage [512]uint8
	spriteStart    [8]uint16
}

var vic VICII

func VIC() *VICII {
	return &vic
}

// Dot returns the beam's current horizontal position (0 to 503), for
// debugging/tracing tools outside this package. It is computed: the
// machine counts bus cycles, and a caller can only look between them, so
// the answer is always a cycle boundary.
func (v *VICII) Dot() uint16 {
	return v.slot * DotsPerCycle
}

// Slot returns which of the line's bus cycles the machine is in, for the
// same callers. This is what the VIC-II actually counts.
func (v *VICII) Slot() uint16 {
	return v.slot
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
	v.slot = 0
	v.setBank()
	v.rasterLine = 0
	v.beamLine = 0
	v.BA = true
	// Raster line 0 is inside the upper border, which the border unit only
	// leaves at the top comparison on line $33/$37. Both flip-flops
	// therefore have to start set, or the first frame paints graphics over
	// the whole upper border until that comparison arrives.
	v.mainBorder = true
	v.verticalBorder = true
	v.gdSequencer = 0
	v.graphicsMode = modeStandardText
	v.multicolor = false
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
	v.baLowCycles = 0
	v.idle = true
	v.rasterCompare = 0
	v.rasterIRQTriggered = false
	v.interruptStatus = 0
	v.interruptEnable = 0
	v.setIRQ(false)
	v.refreshGraphicsPalette()
	v.spriteSpriteCollision = 0
	v.spriteDataCollision = 0
	v.spriteDisplay = 0
	v.spriteExpFF = 0
	v.spriteRow = [8]uint8{}
	v.spriteShape = [8][3]uint8{}
	v.spritePixels = [8][48]uint8{}
	v.spriteStart = [8]uint16{}
	clear(v.spriteCoverage[:])
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
	asserted := (v.interruptStatus & v.interruptEnable & 0x0F) != 0
	if asserted != v.IRQ {
		v.setIRQ(asserted)
	}
}

func (v *VICII) setIRQ(asserted bool) {
	v.IRQ = asserted
	if v == &vic {
		cpu.setInterrupt(sourceVIC, asserted)
	}
}

// setBank works out the base of the VIC-II's 16K fetch window from CIA2's
// port A. Reset establishes it, and the PLA calls it when a write lands on
// that port.
//
// The two bank-select lines are inverted, and the port pins float high when
// configured as inputs, just as the real pull-ups do. They are bits 0 and 1;
// the serial bus drives bits 3 to 5 of the same port, so iec.go can move
// those without disturbing the window.
func (v *VICII) setBank() {
	v.bank = uint16(^effective(cia.cia2.PRA, cia.cia2.DDRA)&0x03) << 14
}

// syncLineVisibility recomputes the cached line visibility flags, and puts
// the beam on the line the vertical counter names. It must be called
// whenever rasterLine is assigned by anything other than VINC, which
// updates both itself.
func (v *VICII) syncLineVisibility() {
	v.beamLine = v.rasterLine
	v.lineVisible = v.rasterLine < firstVBlankLine && v.rasterLine > lastVBlankLine
	v.lineDrawable = v.rasterLine >= renderFirstLine && v.rasterLine < renderLineAfter
}

func (v *VICII) WriteRegister(addr uint16, value uint8) {
	reg := addr & 0x3F
	switch {
	case reg < 0x10:
		// $D000-$D00F, X and Y interleaved. A low byte write leaves the
		// ninth bit, which lives in $D010, alone.
		if reg&1 == 0 {
			v.spriteX[reg>>1] = v.spriteX[reg>>1]&0x100 | uint16(value)
			// An even register is a sprite X, and moving it moves the
			// display window. The odd ones are Y, which only reaches the
			// display through latchSpriteDisplay.
			v.rebuildSpriteCoverage()
		} else {
			v.spriteY[reg>>1] = value
		}
	case reg == 0x10:
		// $D010 is the ninth bit of every sprite's X, so it moves display
		// windows for the same reason the even registers above do.
		v.setSpriteXMSB(value)
		v.rebuildSpriteCoverage()
	case reg == regControl1:
		v.control1 = value
		v.rasterCompare = (v.rasterCompare & 0xFF) | (uint16(value&0x80) << 1)
		v.checkRasterIRQ()
	case reg == 0x12:
		v.rasterCompare = (v.rasterCompare & 0x100) | uint16(value)
		v.checkRasterIRQ()
	case reg == 0x13:
		v.lightpenX = value
	case reg == 0x14:
		v.lightpenY = value
	case reg == 0x15:
		v.spriteEnable = value
	case reg == regControl2:
		v.control2 = value
		v.sampleGraphicsAtWrite(value)
	case reg == 0x17:
		v.spriteExpandY = value
	case reg == regMemPointers:
		v.memPointers = value
	case reg == 0x19:
		v.interruptStatus &^= (value & 0x0F)
		v.updateIRQ()
	case reg == 0x1A:
		v.interruptEnable = value & 0x0F
		v.updateIRQ()
	case reg == 0x1B:
		v.spritePriority = value
	case reg == 0x1C:
		// Whether a shape byte is eight hires pixels or four multicolour
		// ones. That is the shape of a decoded row rather than its
		// colours, so the rows have to be worked out again.
		v.spriteMulticolor = value
		v.decodeSpriteRows()
	case reg == 0x1D:
		// X expansion doubles a sprite's width, so it changes both which
		// dots its window covers and how the row is laid out across
		// them. $D01B, named above, changes how a covered dot is painted
		// but neither of those.
		v.spriteExpandX = value
		v.rebuildSpriteCoverage()
		v.decodeSpriteRows()
	case reg == 0x1E, reg == 0x1F:
		// Collision registers are read-cleared and ignore writes.
	case reg == regBorderColor:
		v.borderColor = value
	case reg <= regBackground3:
		v.background[reg-regBackground0] = value
		v.refreshGraphicsPalette()
	case reg == 0x25:
		v.spriteMC0 = value
	case reg == 0x26:
		v.spriteMC1 = value
	case reg < 0x2F:
		// $D027-$D02E. The graphics sequencer does not read the sprite
		// colours, so unlike the background colours above they do not
		// invalidate the palette.
		v.spriteColor[reg-0x27] = value
	}
}

// backgroundColor is one of the four background colour registers,
// $D021-$D024. Only ECM reaches indices 1-3, through the top two bits of a
// character pointer, so the index is already in range; masking it makes
// that true for any caller and costs nothing.
func (v *VICII) backgroundColor(index uint8) uint8 {
	return v.background[index&3]
}

func (v *VICII) selectedGraphicsMode() uint8 {
	return (v.control1>>4)&0x06 | (v.control2 >> 4 & 0x01)
}

// sampleGraphicsAtWrite handles a $D016 store that moves XSCROLL onto the
// dot the sequencer is about to reload on.
//
// It runs from the CPU's Phi2, which falls at the midpoint of a bus cycle -
// four dots in, always, which is what TestVICStepCycleCPUWriteLandsMidSlot
// pins. So the dot it is asking about is fixed, and only which bus cycle
// this is has to be looked up, and that is the one thing the machine
// counts.
func (v *VICII) sampleGraphicsAtWrite(control2 uint8) {
	if v.slot < reloadFirstSlot || v.slot >= reloadSlotAfter {
		return
	}
	if graphicsReloadPhase(control2) == DotsPerCycle/2 {
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

// noReloadDot is a dot no cycle can advance onto, marking a cycle in
// which the graphics sequencer does not reload.
const noReloadDot = 0xFFFF

// noCompareDot is the same trick for the border comparator: a dot the
// beam never reaches, so a comparison against it never fires.
const noCompareDot = 0xFFFF

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
// reloadDot is the dot in this bus cycle the graphics sequencer takes up
// its g-access result on, or noReloadDot if this cycle has none.
//
// mayReload is whether the cycle is one of the forty that can reload at
// all - slots reloadFirstSlot through reloadSlotAfter-1. The caller knows
// that from the slot number; asking here would mean dividing the beam
// position back down to a slot the caller already has.
//
// The phase cannot be hoisted any further than this. XSCROLL is writable
// mid-line, so a $D016 store in one cycle moves the next cycle's reload
// dot, which is what TestVICXScrollWriteReloadsAtCurrentDot pins.
func (v *VICII) reloadDot(dot uint16, mayReload bool) uint16 {
	if !mayReload {
		return noReloadDot
	}
	// The dotclocks act on dots dot through dot+DotsPerCycle-1, so the
	// phase is matched against those. Exactly one of them is congruent to
	// the reload phase; this is it.
	return dot + ((graphicsReloadPhase(v.control2) - dot) & 7)
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

// nextGraphicsColor shifts one pixel out of the graphics sequencer and
// reports its colour and the index it came from. The shift is the only
// part of this that is genuinely per dot: the colour it lands on was
// decided for the whole cycle by refreshGraphicsPalette, and the pixel
// width by expandGraphicsData when the sequencer last reloaded.
// The index is returned rather than the foreground bit because only the
// sprite compositor reads that, and most dots have no sprite over them -
// graphicsPixel's early-out returns before it would be used. So the
// bit is worked out after that branch instead, where it is read.
//
// Measured null on desktop, twice, on two shapes of graphicsPixel
// two days and one rewrite apart: -0.31% [-0.95, +0.34] when first tried, and +0.15% against
// a 0.73% drift floor on the current dot path. A wide out-of-order core
// issues the discarded ALU work in slots that were idle anyway. It is here
// because per-dot work should be what is read per dot, not for a win.
func (v *VICII) nextGraphicsColor() (byte, uint8) {
	index := uint8(v.gdSequencer >> 14)
	v.gdSequencer <<= 2
	return v.gdColor[index], index
}

// graphicsPixelPlain4 decides the colours of all four dots of a Phi0
// half-phase, none of which a sprite covers. It is graphicsPixel with the
// compositor's whole half removed rather than branched around - no
// coverage load, no early-out, no foreground bit - and with everything the
// four dots agree on lifted out of them.
//
// The caller has to know no sprite covers any of the dots it hands over.
// spriteDisplay being zero is that guarantee - rebuildSpriteCoverage
// clears the table and returns as soon as it sees zero, so nothing can be
// covered - and it can only change when the CPU writes a sprite register,
// which happens between the two half-phases of a bus cycle, never inside
// one. So it is asked once per four dots instead of once per dot.
//
// reloadOffset is how far into the group the sequencer reloads, as the
// cycle's reload dot gives it: 0 to 3, or anything larger for a group
// that does not reload.
//
// The border flip-flop cannot move inside a group either. Only
// borderCompare writes it, and the three slots it can match in are
// outside the display window, which is the only place this path runs; the
// CPU cannot write it either, since a CSEL change only reaches it through
// the next comparison. So it is one test for the group, not one a dot.
//
// That makes a closed border worth answering outright. Nothing reads what
// the sequencer shifts out while it is closed - only that it keeps
// shifting, which is what graphicsPixel's comment is about - so the
// four samples go and the four shifts collapse into one. A reload
// replaces the register rather than shifting into it, so the shifts
// before it are dead too, and only the dots after it have to be counted.
//
// It does not take the dot. Where the pixels land is the write's
// business, and the write is the caller's; with the compositor gone there
// is nothing left here that depends on where along the line the beam is.
//
// TestPlainGroupMatchesPaintWithNoSprites holds the whole of that to four
// graphicsPixel calls.
func (v *VICII) graphicsPixelPlain4(reloadOffset uint16) (byte, byte, byte, byte) {
	if v.mainBorder {
		if reloadOffset < DotsPerCycle/2 {
			v.loadGraphicsData()
			v.gdSequencer <<= 2 * (DotsPerCycle/2 - reloadOffset)
		} else {
			v.gdSequencer <<= 2 * (DotsPerCycle / 2)
		}
		border := v.borderColor
		return border, border, border, border
	}

	var c0, c1, c2, c3 byte
	switch reloadOffset {
	case 0:
		v.loadGraphicsData()
		c0, _ = v.nextGraphicsColor()
		c1, _ = v.nextGraphicsColor()
		c2, _ = v.nextGraphicsColor()
		c3, _ = v.nextGraphicsColor()
	case 1:
		c0, _ = v.nextGraphicsColor()
		v.loadGraphicsData()
		c1, _ = v.nextGraphicsColor()
		c2, _ = v.nextGraphicsColor()
		c3, _ = v.nextGraphicsColor()
	case 2:
		c0, _ = v.nextGraphicsColor()
		c1, _ = v.nextGraphicsColor()
		v.loadGraphicsData()
		c2, _ = v.nextGraphicsColor()
		c3, _ = v.nextGraphicsColor()
	case 3:
		c0, _ = v.nextGraphicsColor()
		c1, _ = v.nextGraphicsColor()
		c2, _ = v.nextGraphicsColor()
		v.loadGraphicsData()
		c3, _ = v.nextGraphicsColor()
	default:
		c0, _ = v.nextGraphicsColor()
		c1, _ = v.nextGraphicsColor()
		c2, _ = v.nextGraphicsColor()
		c3, _ = v.nextGraphicsColor()
	}
	return c0, c1, c2, c3
}

// graphicsPixel decides one pixel's colour, through the border unit and
// the sprite compositor. It returns it rather than writing it: a dot's
// colour is per dot, but the write need not be, and dotclock4 collects
// four of these into one write.
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
func (v *VICII) graphicsPixel(dot uint16) byte {
	graphicsColor, gdIndex := v.nextGraphicsColor()
	// Which sprites cover this dot, decided when the line's windows were
	// last settled rather than by asking all eight here. The mask keeps
	// the index provably inside the table; see spriteCoverage.
	display := v.spriteCoverage[dot&511]

	if display == 0 {
		if v.mainBorder {
			graphicsColor = v.borderColor
		}
		return graphicsColor
	}

	// Past the early-out, so this is only worked out for the dots a sprite
	// actually covers. See nextGraphicsColor.
	isForeground := v.gdForeground&(1<<gdIndex) != 0

	// More than one sprite over the one dot is the rare half, and none of
	// it is here: see graphicsPixelOverlap.
	if display&(display-1) != 0 {
		return v.graphicsPixelOverlap(dot, display, graphicsColor, isForeground)
	}

	// One sprite over the dot is the common case, and it does not need
	// any of the machinery the overlap path carries. Nothing can win the
	// pixel ahead of it, so there is no first-hit bookkeeping; nothing
	// can share the dot with it, so there is no sprite-sprite collision.
	// What is left is the dot's own value, one collision register and the
	// priority bit.
	i := uint8(bits.TrailingZeros8(display))
	val := v.spritePixels[i][dot-v.spriteStart[i]]
	if val == spriteDotNone {
		if v.mainBorder {
			graphicsColor = v.borderColor
		}
		return graphicsColor
	}

	mask := spriteBit(i)
	if isForeground && mask&^v.spriteDataCollision != 0 {
		v.raiseSpriteDataCollision(mask)
	}

	if v.mainBorder {
		return v.borderColor
	}
	if v.spritePriority&mask == 0 || !isForeground {
		return spriteDotColor(v, val, i)
	}
	return graphicsColor
}

// raiseSpriteDataCollision records that sprite data met foreground
// graphics data, in $D01F and the interrupt latch.
//
// Both callers test the register before reaching here, for the reason
// graphicsPixelOverlap gives: a collision is news once and then
// background, so the store and the interrupt update - neither of them
// small, and updateIRQ is a call of its own - are work the dot path
// should carry a branch to rather than a copy of.
func (v *VICII) raiseSpriteDataCollision(mask uint8) {
	v.spriteDataCollision |= mask
	v.interruptStatus |= 0x08
	v.updateIRQ()
}

// raiseSpriteSpriteCollision records that two sprites painted the same
// dot, in $D01E and the interrupt latch.
func (v *VICII) raiseSpriteSpriteCollision(mask uint8) {
	v.spriteSpriteCollision |= mask
	v.interruptStatus |= 0x04
	v.updateIRQ()
}

// graphicsPixelOverlap decides a dot that more than one sprite covers.
// The first hit wins the pixel, and every sprite that painted the dot
// goes into the sprite-sprite collision register.
//
// It is out of line from graphicsPixel because it is the rare half, and
// the expensive one: a loop over the coverage mask, two collision
// registers, and first-hit bookkeeping that neither of the other two
// paths needs. Sprites have to be placed over one another to reach it at
// all. Kept here it costs the dots that need it a call and the dots that
// do not nothing, which is what leaves graphicsPixel small enough for a
// caller to take four of it.
func (v *VICII) graphicsPixelOverlap(dot uint16, display uint8, graphicsColor byte, isForeground bool) byte {
	d := dot
	priorityReg := v.spritePriority

	var (
		hitCount             int
		currentHitMask       uint8
		topSpriteColor       byte
		topSpritePriorityBit bool
	)

	// Lowest numbered sprite first, because the first hit wins the pixel:
	// walking the coverage mask from its low bit visits them in the same
	// order the range tests did.
	for remaining := display; remaining != 0; remaining &= remaining - 1 {
		i := uint8(bits.TrailingZeros8(remaining))
		mask := spriteBit(i)

		// The dot is inside this sprite's window - that is what coverage
		// means - so how far into it says which of the row's dots this
		// is, and decodeSpriteRow has already worked out what that dot
		// paints. Expansion is folded into the row, so this is a plain
		// offset either way.
		val := v.spritePixels[i][d-v.spriteStart[i]]
		if val == spriteDotNone {
			continue
		}

		color := spriteDotColor(v, val, i)

		hitCount++
		currentHitMask |= mask
		if hitCount == 1 {
			topSpriteColor = color
			topSpritePriorityBit = (priorityReg & mask) != 0 // $D01B priority
		}
	}

	// Both registers are latched until read, so once a sprite's bit is in
	// one every later dot leaves it exactly as it was. Testing before
	// storing keeps a read-modify-write off VICII state on every covered
	// dot, which is what the common case is - a collision is news once and
	// then background.
	if hitCount > 1 && currentHitMask&^v.spriteSpriteCollision != 0 {
		v.raiseSpriteSpriteCollision(currentHitMask)
	}

	if hitCount > 0 && isForeground && currentHitMask&^v.spriteDataCollision != 0 {
		v.raiseSpriteDataCollision(currentHitMask)
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

	return finalColor
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
	case 0x10:
		return v.spriteXMSBRegister()
	case 0x13:
		return v.lightpenX
	case 0x14:
		return v.lightpenY
	case 0x15:
		return v.spriteEnable
	case 0x17:
		return v.spriteExpandY
	case 0x1B:
		return v.spritePriority
	case 0x1C:
		return v.spriteMulticolor
	case 0x1D:
		return v.spriteExpandX
	case 0x25:
		return v.spriteMC0
	case 0x26:
		return v.spriteMC1
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
	case reg < 0x10:
		// $D000-$D00F, X and Y interleaved.
		if reg&1 == 0 {
			return uint8(v.spriteX[reg>>1])
		}
		return v.spriteY[reg>>1]
	case reg <= regBackground3:
		// $D021-$D024. Everything from $D010 to $D020 returned from the
		// switch above, so reg cannot be below $D021 here - but that
		// switch matches by equality, and an equality test that falls
		// through leaves no range behind for the compiler to carry. The
		// mask says what it cannot infer, and costs nothing: the
		// subtraction is already here and the result is already 0-3.
		return v.background[(reg-regBackground0)&3]
	case reg < 0x2F:
		// $D027-$D02E, in range for the same unprovable reason.
		return v.spriteColor[(reg-0x27)&7]
	}
	return 0xFF
}

// StepFrame advances the VIC-II, and therefore the rest of the machine it
// clocks, by exactly one PAL frame: RasterLinesPerFrame raster lines of
// CyclesPerLine bus cycles each.
//
// The beam must be at the start of a raster line. Reset leaves it there
// and a frame is a whole number of lines, so a caller driving frames never
// has to think about it; one that has stepped cycles must finish the line
// first. This is a requirement rather than something handled, because
// handling it meant carrying a second loop that walked the frame a cycle
// at a time, and nothing but a test ever reached it.
//
// It remains a relative step in the raster line: a frame begun on line 45
// ends on line 45. Only the position within the line is pinned.
func (v *VICII) StepFrame() {
	if v.slot != 0 {
		panic("tiny64: StepFrame needs the beam at the start of a line; " +
			"finish the line with StepCycle first")
	}

	for range RasterLinesPerFrame {
		v.stepLine()
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
// position can actually reach: the border comparisons and the g-access
// commit only ever trigger on specific dots within a cycle (see each
// function's comment), so the six interior dots' bodies are smaller
// besides.
// renderWindowIsCycleAligned fails to compile if the active pixel sink's
// render window does not start and end on a bus-cycle boundary. stepCycle
// decides once per cycle whether its eight dots reach the screen, which is
// only the same answer for all eight while that holds; an unaligned edge
// would put half a cycle inside the window and silently drop those dots.
//
// Converting a negative constant to uint is the error: a remainder of zero
// gives uint(0), anything else gives uint of a negative number.
const (
	_ = uint(0 - renderFirstDot%DotsPerCycle)
	_ = uint(0 - renderDotAfter%DotsPerCycle)
	_ = uint(0 - VisibleDotsPerLine%DotsPerCycle)
)

// slot is which of the line's 63 bus cycles this is - a property of the
// cycle, not of any one of its dots. The caller counts it rather than this
// recovering it from the beam, because a loop that knows where it is does
// not have to ask: StepFrame divides once a frame instead of 19,656 times.
// cycleDraw runs one bus cycle that paints: four dots, the VIC-II's own
// phase, the CPU and what it clocks, four more dots, the CPU's phase.
//
// The two halves are written out rather than shared, because nothing in
// this path inlines: a helper holding them would be two more real calls per
// bus cycle, 39,312 a frame, to save eight lines here.
func (v *VICII) cycleDraw(slot, dot uint16, borderSlot, mayReload bool) {
	// The sequencer's reload dot is fixed for the cycle, for the reason
	// reloadDot gives.
	reload := v.reloadDot(dot, mayReload)

	v.dotclock4(dot, reload, borderSlot)
	v.phi0low(slot)

	// The CPU runs first, since the VIC has just handed it the bus.
	cpu.TickPhi2()

	// The CIAs clock on that Phi2's falling edge, so they run after the
	// CPU and see whatever its bus cycle wrote.
	cia.cia1.tick(sourceCIA1)
	cia.cia2.tick(sourceCIA2)

	// The IEC devices run last, because what they find on the bus is
	// whatever CIA2 has just driven onto it.
	iecTick()

	v.dotclock4(dot+4, reload, borderSlot)
	// The bus cycle is over. This is the machine's only beam counter, and
	// it moves once here rather than eight times through the dot path.
	v.slot = slot + 1
	v.phi0high(slot)

}

// cycleDrawDisplay is cycleDraw for the interior of the display window,
// where phi0lowDisplay stands in for phi0low. The body is spelled out
// rather than shared with cycleDraw for the reason cycleDraw gives: nothing
// in this path inlines, so a shared helper would be real calls.
func (v *VICII) cycleDrawDisplay(dot uint16) {
	reload := v.reloadDot(dot, true)

	// Asked once for the half-phase, not once a dot: see
	// graphicsPixelPlain4 for why four dots can share the answer. dotclock4
	// asks the same question, but reaching it to be asked is a call, and
	// this is the path that runs on every slot of the display window.
	if v.spriteFreeGroup(dot) {
		c0, c1, c2, c3 := v.graphicsPixelPlain4(reload - dot)
		writePixels4ToBuffer(dot, v.beamLine, c0, c1, c2, c3)
	} else {
		v.dotclock4(dot, reload, false)
	}
	v.phi0lowDisplay()

	cpu.TickPhi2()
	cia.cia1.tick(sourceCIA1)
	cia.cia2.tick(sourceCIA2)
	iecTick()

	if v.spriteFreeGroup(dot + 4) {
		c0, c1, c2, c3 := v.graphicsPixelPlain4(reload - (dot + 4))
		writePixels4ToBuffer(dot+4, v.beamLine, c0, c1, c2, c3)
	} else {
		v.dotclock4(dot+4, reload, false)
	}
	v.slot = dot/DotsPerCycle + 1
	v.phi0highDisplay()

}

// cycleBlank runs one bus cycle whose dots reach nothing: the same
// sequence, with the beam stepping over the dots instead of shifting them
// out. Everything the VIC-II and the CPU do in a cycle still happens.
func (v *VICII) cycleBlank(slot, dot uint16) {
	v.phi0low(slot)
	cpu.TickPhi2()
	cia.cia1.tick(sourceCIA1)
	cia.cia2.tick(sourceCIA2)
	iecTick()
	// The bus cycle is over. This is the machine's only beam counter, and
	// it moves once here rather than eight times through the dot path.
	v.slot = slot + 1
	v.phi0high(slot)

}

// drawRun paints the bus cycles from slot through to-1. None of them is a
// slot the border comparator can match, which is what makes it a run.
func (v *VICII) drawRun(from, to uint16, mayReload bool) {
	for slot := from; slot < to; slot++ {
		v.cycleDraw(slot, slot*DotsPerCycle, false, mayReload)
	}
}

// blankRun runs the bus cycles from slot through to-1 without painting.
func (v *VICII) blankRun(from, to uint16) {
	for slot := from; slot < to; slot++ {
		v.cycleBlank(slot, slot*DotsPerCycle)
	}
}

// stepLine advances the machine by one raster line: CyclesPerLine bus
// cycles, starting at slot 0.
//
// A line has more in common across it than a bus cycle does, and this is
// where that is spent. Which slots paint and which can match a border
// comparison are fixed for every line, so the line is walked as runs with
// the answer built into each rather than asked per cycle: slots 0 to 50
// paint, slots 6, 44 and 46 are the three the comparator can match, and
// everything from 51 on is blanked.
//
// Drawability is read once, at the top. That is sound because every slot
// that paints is below renderSlotAfter, and VINC - where the raster
// counter moves and the flag is recomputed - is at slot 53, above it. So
// no line ever changes its mind about painting while it still has painting
// left to do. TestLineDrawabilityIsSettledBeforeAnythingPaints pins it.
// endLine moves the beam to the next row. This used to be a test in every
// bus cycle asking whether the beam had run off the end of the line;
// counting in slots makes it a fact about where the loop stopped.
func (v *VICII) endLine() {
	v.slot = 0
	v.beamLine++
	if v.beamLine >= RasterLinesPerFrame {
		v.beamLine = 0
	}
}

func (v *VICII) stepLine() {
	if !v.lineDrawable {
		v.blankRun(0, CyclesPerLine)
		v.endLine()
		return
	}

	// The reload window opens at slot 6, which is also the first slot the
	// border comparator can match, and closes after 45 - one slot before
	// the comparator's last. So the runs already divide on it, and no run
	// has to ask.
	v.drawRun(0, borderSlotLeft, false)
	v.cycleDraw(borderSlotLeft, borderSlotLeft*DotsPerCycle, true, true)
	for dot := uint16(displayFirstSlot * DotsPerCycle); dot < displaySlotAfter*DotsPerCycle; dot += DotsPerCycle {
		v.cycleDrawDisplay(dot)
	}
	v.cycleDraw(borderSlotRight38, borderSlotRight38*DotsPerCycle, true, true)
	v.drawRun(borderSlotRight38+1, borderSlotRight40, true)
	v.cycleDraw(borderSlotRight40, borderSlotRight40*DotsPerCycle, true, false)
	v.drawRun(borderSlotRight40+1, renderSlotAfter, false)
	v.blankRun(renderSlotAfter, CyclesPerLine)
	v.endLine()
}

// stepCycle advances the machine by one bus cycle, whichever slot it is
// in. StepFrame walks whole lines instead; this is what a caller stepping
// a cycle at a time gets, and what StepFrame falls back to when the beam
// is not parked on a line boundary.
//
// slot is which of the line's 63 bus cycles this is - a property of the
// cycle, not of any one of its dots.
func (v *VICII) stepCycle(slot uint16) {
	borderSlot := slot == borderSlotLeft || slot == borderSlotRight38 ||
		slot == borderSlotRight40
	// Whether this cycle's dots reach the screen. The window's edges are
	// bus-cycle aligned - renderWindowIsCycleAligned enforces it - so all
	// eight dots of a cycle are inside it or all eight are outside.
	onScreen := slot >= renderFirstSlot && slot < renderSlotAfter &&
		slot < visibleSlots
	dot := slot * DotsPerCycle
	if v.lineDrawable && onScreen {
		v.cycleDraw(slot, dot, borderSlot,
			slot >= reloadFirstSlot && slot < reloadSlotAfter)
	} else {
		v.cycleBlank(slot, dot)
	}

	// stepLine wraps the line when its run of slots ends. Stepping one
	// cycle at a time, the last slot is where that falls.
	if slot == CyclesPerLine-1 {
		v.slot = 0
		v.beamLine++
		if v.beamLine >= RasterLinesPerFrame {
			v.beamLine = 0
		}
	}
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
	v.stepCycle(v.slot)
}

// StepFrame advances the singleton machine by exactly one PAL frame. See
// VICII.StepFrame for where in the frame it starts and stops.
func StepFrame() {
	vic.StepFrame()
}

// dotclockBorder4 is the four dots of a Phi0 half-phase in one of the
// three slots a line the border comparator can match in.
//
// The comparison itself stays per dot, because the dot it fires on is
// painted with the state it just set. What the comparator asks before
// comparing does not: which pair of dots CSEL selects, and whether the
// line can match at all, are the same for all four. The CPU only reaches
// $D016 between half-phases and lineVisible is settled at VINC, so
// neither can move under the group.
// Each dot takes up the g-access result, if this is the dot XSCROLL
// selects; asks the comparator, against the pair chosen above; and
// decides its colour. The four are written out rather than called,
// because a per-dot function here is a real call and this is the path
// that has four of them. The write is dotclock4's, since it is one write
// for the group.
func (v *VICII) dotclockBorder4(dot, reload uint16) (byte, byte, byte, byte) {
	left, right := v.borderComparePair()

	// Whether any sprite is displayed is the group's question here as it
	// is everywhere else - see dotclock4 - and answering it once is what
	// lets a group none covers decide its dots inline rather than call
	// the compositor four times.
	//
	// What the group cannot answer is the border flip-flop, which is the
	// one thing these three slots a line exist for: the comparator can
	// move it between one dot and the next. So the plain run below still
	// asks it per dot, where graphicsPixelPlain4 asks it once.
	if v.spriteFreeGroup(dot) {
		if dot == reload {
			v.loadGraphicsData()
		}
		v.borderCompare(dot, left, right)
		c0 := v.graphicsPixelPlain()

		if dot+1 == reload {
			v.loadGraphicsData()
		}
		v.borderCompare(dot+1, left, right)
		c1 := v.graphicsPixelPlain()

		if dot+2 == reload {
			v.loadGraphicsData()
		}
		v.borderCompare(dot+2, left, right)
		c2 := v.graphicsPixelPlain()

		if dot+3 == reload {
			v.loadGraphicsData()
		}
		v.borderCompare(dot+3, left, right)
		c3 := v.graphicsPixelPlain()

		return c0, c1, c2, c3
	}

	// The comparator runs before the paint: the dot a comparison fires on
	// is painted with the state it just set, not the one before.
	if dot == reload {
		v.loadGraphicsData()
	}
	v.borderCompare(dot, left, right)
	c0 := v.graphicsPixel(dot)

	if dot+1 == reload {
		v.loadGraphicsData()
	}
	v.borderCompare(dot+1, left, right)
	c1 := v.graphicsPixel(dot + 1)

	if dot+2 == reload {
		v.loadGraphicsData()
	}
	v.borderCompare(dot+2, left, right)
	c2 := v.graphicsPixel(dot + 2)

	if dot+3 == reload {
		v.loadGraphicsData()
	}
	v.borderCompare(dot+3, left, right)
	c3 := v.graphicsPixel(dot + 3)

	return c0, c1, c2, c3
}

// spriteFreeGroup reports that no sprite covers any of a half-phase's
// four dots, which is what lets the group skip the compositor outright.
//
// The compositor's own early-out asks the same thing a dot at a time, and
// pays a call to do it. Asked here it is four loads folded together, or
// one when no sprite is displayed at all - and a sprite is 24 dots wide,
// so even on a line carrying eight of them most groups are not covered by
// any.
//
// Masking to a multiple of four keeps all four indices provably inside
// the table: every group starts on one, so the mask changes no answer
// that a real caller asks for.
func (v *VICII) spriteFreeGroup(dot uint16) bool {
	if v.spriteDisplay == 0 {
		return true
	}
	d := dot & (511 &^ 3)
	return v.spriteCoverage[d]|v.spriteCoverage[d+1]|
		v.spriteCoverage[d+2]|v.spriteCoverage[d+3] == 0
}

// graphicsPixelPlain decides one dot's colour with no sprite over it: the
// compositor's whole half removed rather than branched around, and the
// border still asked, because the group it belongs to cannot answer that
// for it. Only dotclockBorder4's plain run wants this - anywhere else the
// flip-flop holds for the group and graphicsPixelPlain4 lifts it out.
func (v *VICII) graphicsPixelPlain() byte {
	graphicsColor, _ := v.nextGraphicsColor()
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	return graphicsColor
}

// dotclock4 is the four dots of one Phi0 half-phase. Every caller wants
// exactly four of them with the same reload dot and the same borderSlot,
// and it is the only way to paint at all: there is no single-dot step.
//
// A per-dot step was too big to inline - the paint was a real call inside
// it - so those four dots were four calls, and that dispatch measured
// about 13% of the frame, near enough the same share on all six demos.
//
// Both of its questions are the same for all four dots, so they are asked
// once here instead of once a dot. borderSlot is fixed for the cycle
// and true in three slots of 63. A bus cycle reloads at most once, so at
// most one of these four dots is the reload dot, and reload-dot says which
// - unsigned, so a reload in the cycle's other half, or none at all, comes
// out >= 4 rather than needing a test of its own.
//
// Four predictable branches are not expensive on an out-of-order core.
// What this removes is the three extra calls and the per-dot work behind
// them, which is why it is worth measuring rather than counting.
//
// The write is the third thing the four dots share. Deciding a colour is
// per dot; putting it somewhere is not, and the four dots of a half-phase
// are four consecutive pixels of one raster line. So the colours are
// collected and handed to the sink together, so the row offset and the
// bounds check are worked out once for the group - and, since nothing
// between the dots can read the frame buffer or move the beam off this
// line, holding three colours back until the fourth is decided changes no
// answer.
func (v *VICII) dotclock4(dot, reload uint16, borderSlot bool) {
	var c0, c1, c2, c3 byte
	if borderSlot {
		// Three slots a line, and the only ones whose dots are not all
		// alike: see dotclockBorder4.
		c0, c1, c2, c3 = v.dotclockBorder4(dot, reload)
		writePixels4ToBuffer(dot, v.beamLine, c0, c1, c2, c3)
		return
	}

	// Whether a sprite covers any of these four dots belongs to the group,
	// for the reason graphicsPixelPlain4 gives, and a group none covers
	// has no use for the compositor: no coverage load a dot, and no call a
	// dot either, since what decides a plain group is small enough to
	// inline where the compositor is not.
	if v.spriteFreeGroup(dot) {
		c0, c1, c2, c3 = v.graphicsPixelPlain4(reload - dot)
		writePixels4ToBuffer(dot, v.beamLine, c0, c1, c2, c3)
		return
	}

	switch reload - dot {
	case 0:
		v.loadGraphicsData()
		c0 = v.graphicsPixel(dot)
		c1 = v.graphicsPixel(dot + 1)
		c2 = v.graphicsPixel(dot + 2)
		c3 = v.graphicsPixel(dot + 3)
	case 1:
		c0 = v.graphicsPixel(dot)
		v.loadGraphicsData()
		c1 = v.graphicsPixel(dot + 1)
		c2 = v.graphicsPixel(dot + 2)
		c3 = v.graphicsPixel(dot + 3)
	case 2:
		c0 = v.graphicsPixel(dot)
		c1 = v.graphicsPixel(dot + 1)
		v.loadGraphicsData()
		c2 = v.graphicsPixel(dot + 2)
		c3 = v.graphicsPixel(dot + 3)
	case 3:
		c0 = v.graphicsPixel(dot)
		c1 = v.graphicsPixel(dot + 1)
		c2 = v.graphicsPixel(dot + 2)
		v.loadGraphicsData()
		c3 = v.graphicsPixel(dot + 3)
	default:
		c0 = v.graphicsPixel(dot)
		c1 = v.graphicsPixel(dot + 1)
		c2 = v.graphicsPixel(dot + 2)
		c3 = v.graphicsPixel(dot + 3)
	}
	writePixels4ToBuffer(dot, v.beamLine, c0, c1, c2, c3)
}

// phi0low runs after the first four dots of every 8-dot cycle: while the VIC-II is
// in charge of the bus, it performs its own memory reads here (section
// 3.7.2 of the VIC Article). Sprites are not implemented yet.
//
// cycleRaster0/cycleRaster30/cycleIsBadLine/cycleIsCAccess are inlined
// directly here (each had exactly one call site, unconditional or nearly
// so) to remove function-call overhead from the frame fast path.
// resolveVerticalBorder is the tail of section 3.9 rule 1 that both column
// widths share: reaching a left comparison value samples the vertical
// border flip-flop against RSEL and DEN, and only opens the main border if
// the vertical one is open too.
func (v *VICII) resolveVerticalBorder() {
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

// borderCompare is section 3.9 rule 1: one comparator against the beam,
// with CSEL choosing which pair of values it matches. It runs on every dot,
// which is what the hardware does - both values of a pair are a single dot,
// and the two pairs are one dot apart in phase, so no per-slot decode can
// reach all four.
//
// The left value is the first displayed dot and the right value the first
// border dot, so the display window is half-open: [31, 351) in 40 columns
// and [38, 342) in 38, rebased here by the same +17 the rest of this file
// carries. Narrowing pulls both ends in by a cell and then back by a dot,
// which is why the picture loses 7 dots on the left and 9 on the right
// rather than 8 and 8.
func (v *VICII) borderCompare(dot, left, right uint16) {
	if dot == right {
		v.mainBorder = true
	} else if dot == left {
		v.resolveVerticalBorder()
	}
}

// borderComparePair is the pair of dots the comparator matches against:
// the first displayed dot and the first border dot, with CSEL choosing
// which width's pair that is. A line the beam cannot see matches neither,
// which is noCompareDot's whole job - a dot no beam reaches, so the
// per-dot comparison needs no visibility test of its own.
//
// Both answers belong to the cycle rather than the dot, which is why they
// are asked here and passed down. CSEL cannot change under a group: the
// CPU only reaches $D016 between half-phases.
func (v *VICII) borderComparePair() (left, right uint16) {
	if !v.lineVisible {
		return noCompareDot, noCompareDot
	}
	if v.control2&csel != 0 {
		return leftComp40, rightEdge40
	}
	return leftComp38, rightEdge38
}

// phi0lowDisplay is phi0low for the interior of the display window, slots
// displayFirstSlot through displaySlotAfter-1. Every slot test in phi0low
// has the same answer across that run: none of the one-off cycles is in it,
// and the three ranges - BA on a Bad Line, the c-access and the g-access -
// all cover it whole. So the run's phase is this, with the answers built
// in rather than asked 37 times a line.
//
// It takes no slot: with every test answered, nothing in it depends on
// which of the 37 cycles this is.
//
// It must stay in step with phi0low; TestPhi0LowDisplayMatchesPhi0Low walks
// the run both ways and compares.
func (v *VICII) phi0lowDisplay() {
	// Bad Line state is not hoistable even here: YSCROLL is writable
	// mid-line, so a $D011 store moves the condition between one cycle and
	// the next.
	if v.rasterLine == badLineRasterStart && v.control1&0x10 != 0 {
		v.denLatch = true
	}

	yscroll := v.control1 & 0x07
	badLine := v.rasterLine >= badLineRasterStart && v.rasterLine <= badLineRasterEnd &&
		uint8(v.rasterLine)&0x07 == yscroll && v.allowBadLine
	v.badLine = badLine
	if badLine {
		v.idle = false
	}

	// No sprite can pull BA low here: spriteBASlotMask is zero below slot
	// 44, because the sprite fetch block runs at the end of the line and
	// feeds the next one. TestDisplayRunHasNoSpriteBA holds that.
	ba := !badLine
	v.BA = ba
	if ba {
		v.baLowCycles = 0
	} else if v.baLowCycles <= baWarningCycles {
		v.baLowCycles++
	}

	v.cycleGAccess()
}

func (v *VICII) phi0low(slot uint16) {
	// slot indexes the 8-dot bus cycles across a line. The article's cycle
	// numbering starts 10 slots later (its cycle N is our slot N-11, mod
	// 63), so the constants below are the article's rebased onto slot.

	// The raster counter increments on VINC, not at the first visible dot:
	// VINC fires a cycle after video blanks, and the beam only reaches the
	// left edge after the sync pulse, retrace and back porch that follow
	// it. Everything the CPU times off the raster IRQ hangs on this being
	// in the right place - a program that syncs here and then counts
	// cycles to a $D016 write inherits any error in it, which is how a
	// constant offset in the border unit's comparisons can look like a
	// border unit bug.
	//
	// The framebuffer cannot tell: dots 424 to 503 are past
	// VisibleDotsPerLine, so no painted dot ever sees the old value.
	if slot == vincSlot {
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
	} else if slot == vincSlot+1 && v.rasterLine == 0 {
		// Raster line 0 is compared in cycle 2; every other line in cycle 1.
		v.checkRasterIRQ()
	}

	// cycleRaster0: resets VCBase at the start of raster line 0, on the
	// VINC that begins it.
	if v.rasterLine == 0 && slot == vincSlot {
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
	ba := !(slot >= 1 && slot <= 43 && badLine) && !v.spriteDMAStall(slot)
	v.BA = ba

	// How far into that three cycle lead this cycle is, counted here
	// because it is part of driving BA rather than something Phi2 works
	// out again from a pin. It saturates one past the warning: nothing
	// asks a finer question than whether the warning has expired.
	if ba {
		v.baLowCycles = 0
	} else if v.baLowCycles <= baWarningCycles {
		v.baLowCycles++
	}

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
// slot is currently DMA active, and so is holding BA low. It asks that of
// all eight sprites at once, so it tests the display set as a whole byte
// rather than through spriteUnderDMA.
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
	enable := v.spriteEnable
	expandY := v.spriteExpandY
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
			v.spriteYPos(i) == line {
			v.spriteDisplay |= mask
			v.spriteRow[i] = 0
			v.spriteExpFF |= mask
		}
	}
	v.rebuildSpriteCoverage()
}

// rebuildSpriteCoverage works out which dots each displayed sprite covers,
// so that the dot path can ask one table lookup instead of running eight
// range tests.
//
// The tests it replaces are overwhelmingly negative. A sprite is 24 dots
// wide on a 504 dot line, so on a line carrying all eight the compositor
// used to run 3,240 range tests to find at most 192 covered dots; measured
// over a frame of uncle-agnus-mcfungus, 671,895 iterations found 39,816
// covered dots, and 94% of the work was discarded. Each discarded test
// still loaded the sprite's X from two register arrays, reassembled its
// ninth bit from $D010, and recomputed (24 + x) % DotsPerLine - none of
// which can change between one dot and the next.
//
// Building the table costs at most 384 writes, one per covered dot, and
// the window is clamped at the end of the line rather than wrapped onto
// the next, which is what the range test it replaces did.
//
// Being derived state, it has to be rebuilt wherever its inputs change,
// and the four inputs reach only three places between them:
//
//   - the sprite X registers and their ninth bits in $D010, written in
//     exactly one place, WriteRegister's reg <= 0x10 case;
//   - $D01D's expansion bits, likewise, in the reg < regBorderColor case;
//   - spriteDisplay, changed only by latchSpriteDisplay, which ends by
//     calling this, and cleared by Reset, which clears the table too.
//
// Four inputs collapsing to three sites is a property of how the register
// arrays are written today - each has a single choke point - rather than
// anything this arranges, so it is worth saying out loud: a second writer
// of any of them would need a fourth call, and would not announce itself.
// TestSpriteCoverageStaysConsistentAcrossAFrame is what would catch that,
// by rebuilding after every bus cycle of a live frame and comparing.
//
// Rebuilding on the write rather than latching once a line is what keeps a
// mid-line write to a sprite's position taking effect on the next dot, as
// it did when the registers were read per dot.
func (v *VICII) rebuildSpriteCoverage() {
	clear(v.spriteCoverage[:])
	if v.spriteDisplay == 0 {
		return
	}
	for i := uint8(0); i < 8; i++ {
		if !v.spriteUnderDMA(i) {
			continue
		}
		mask := spriteBit(i)

		// spriteStartDot places the sprite by its leftmost dot, 24 dots
		// left of the display window's first column, and carries the
		// wrap; spriteWidth doubles it for $D01D.
		start := v.spriteStartDot(i)
		v.spriteStart[i] = start

		end := min(start+v.spriteWidth(i), DotsPerLine)
		for dot := start; dot < end; dot++ {
			v.spriteCoverage[dot] |= mask
		}
	}
}

// spriteBit is the bit sprite i occupies in every per-sprite mask
// register - $D010, $D015, $D017, $D01B, $D01C, $D01D, $D01E, $D01F - and
// in the internal spriteDisplay and spriteExpFF sets. This is the only
// place that mapping is written down.
func spriteBit(i uint8) uint8 { return 1 << i }

// spriteHiresDots and spriteMulticolorDots turn one byte of a sprite's
// shape into the dots it paints, in the encoding decodeSpriteRow uses.
// Both are eight dots wide, because a byte is eight hires pixels or four
// multicolour pixels and a multicolour pixel is two dots.
//
// Tables rather than shifting per dot: a row is three bytes, so decoding
// it is three copies of eight bytes instead of twenty-four bit
// extractions.
var (
	spriteHiresDots      = buildSpriteHiresDots()
	spriteMulticolorDots = buildSpriteMulticolorDots()
)

func buildSpriteHiresDots() [256][8]uint8 {
	var table [256][8]uint8
	for b := range table {
		for i := range table[b] {
			if b&(0x80>>i) != 0 {
				table[b][i] = spriteDotOwn
			}
		}
	}
	return table
}

func buildSpriteMulticolorDots() [256][8]uint8 {
	var table [256][8]uint8
	for b := range table {
		for pair := range 4 {
			// Pairs run high bits first, and each paints two dots.
			val := uint8(b>>(6-2*pair)) & 0x03
			table[b][pair*2] = val
			table[b][pair*2+1] = val
		}
	}
	return table
}

// The dot values decodeSpriteRow emits. They are the multicolour bit pair
// verbatim, which is what lets the multicolour table be the pairs
// themselves: 00 is transparent, 01 is $D025, 10 is the sprite's own
// colour and 11 is $D026. A hires pixel is transparent or the sprite's
// own colour, so it uses the same two values.
const (
	spriteDotNone = 0
	spriteDotMC0  = 1
	spriteDotOwn  = 2
	spriteDotMC1  = 3
)

// spriteDotColor turns a decoded dot value into the colour it paints.
//
// The value names one of the four colour registers rather than holding a
// colour, which is what lets decodeSpriteRow run once a line while a
// mid-line write to $D025, $D026 or $D027-$D02E still lands on the dots
// after it.
func spriteDotColor(v *VICII, val, i uint8) byte {
	switch val {
	case spriteDotMC0:
		return v.spriteMC0 & 0x0F
	case spriteDotOwn:
		return v.spriteColor[i] & 0x0F
	default:
		return v.spriteMC1 & 0x0F
	}
}

// decodeSpriteRow works out the dots sprite i paints, once for the line,
// so that graphicsPixel can index the answer instead of deriving it
// for every dot the sprite covers.
//
// What it does not bake in is colour. The values are which of the four
// colour registers a dot takes, not the colour itself, so a mid-line
// write to $D025, $D026 or $D027-$D02E still lands - those registers are
// read where the dot is painted. Only the two registers that change the
// shape of the row rather than its colours have to invalidate this:
// $D01C, which decides whether a byte is eight pixels or four, and
// $D01D, which doubles their width.
func (v *VICII) decodeSpriteRow(i uint8) {
	mask := spriteBit(i)
	table := &spriteHiresDots
	if v.spriteMulticolor&mask != 0 {
		table = &spriteMulticolorDots
	}
	row := v.spritePixels[i][:]
	if v.spriteExpandX&mask == 0 {
		for b, shape := range v.spriteShape[i] {
			copy(row[b*8:b*8+8], table[shape][:])
		}
		return
	}
	// Expanded, so every dot is painted twice and the row is 48 long.
	for b, shape := range v.spriteShape[i] {
		dots := &table[shape]
		for j, val := range dots {
			row[b*16+j*2] = val
			row[b*16+j*2+1] = val
		}
	}
}

// decodeSpriteRows redecodes every sprite under DMA, for the two register
// writes that change how a row is laid out without changing the bytes it
// came from. Redecoding a whole row mid-line is right: the dots already
// painted are in the frame buffer, and only later lookups see the change.
func (v *VICII) decodeSpriteRows() {
	for i := uint8(0); i < 8; i++ {
		if v.spriteUnderDMA(i) {
			v.decodeSpriteRow(i)
		}
	}
}

// spriteXMSBRegister reassembles $D010 from the ninth bit of each sprite's
// X. The register has no storage of its own; this and setSpriteXMSB are the
// only places the two representations meet, and both run only when the CPU
// touches $D010.
func (v *VICII) spriteXMSBRegister() uint8 {
	var m uint8
	for i := range v.spriteX {
		if v.spriteX[i]&0x100 != 0 {
			m |= spriteBit(uint8(i))
		}
	}
	return m
}

// setSpriteXMSB distributes a write to $D010 over the ninth bit of each
// sprite's X, leaving the low eight bits alone.
func (v *VICII) setSpriteXMSB(value uint8) {
	for i := range v.spriteX {
		if value&spriteBit(uint8(i)) != 0 {
			v.spriteX[i] |= 0x100
		} else {
			v.spriteX[i] &^= 0x100
		}
	}
}

// The sprite properties, each naming one register's meaning for one sprite
// so that no caller has to know which bit or which address it came from.

func (v *VICII) spriteXPos(i uint8) uint16 { return v.spriteX[i] }
func (v *VICII) spriteYPos(i uint8) uint8  { return v.spriteY[i] }

// spriteColorOf is sprite i's $D027+i colour. The register stores the whole
// written byte - $D027 reads back exactly what was written - and only the
// low nibble reaches the screen.
func (v *VICII) spriteColorOf(i uint8) uint8 { return v.spriteColor[i] & 0x0F }

func (v *VICII) spriteEnabled(i uint8) bool        { return v.spriteEnable&spriteBit(i) != 0 }
func (v *VICII) spriteExpandedX(i uint8) bool      { return v.spriteExpandX&spriteBit(i) != 0 }
func (v *VICII) spriteExpandedY(i uint8) bool      { return v.spriteExpandY&spriteBit(i) != 0 }
func (v *VICII) spriteIsMulticolor(i uint8) bool   { return v.spriteMulticolor&spriteBit(i) != 0 }
func (v *VICII) spriteBehindGraphics(i uint8) bool { return v.spritePriority&spriteBit(i) != 0 }

// spriteUnderDMA reports whether sprite i is in the set latched for the
// next raster line, which is what decides both whether it is drawn and
// whether it steals bus cycles.
func (v *VICII) spriteUnderDMA(i uint8) bool { return v.spriteDisplay&spriteBit(i) != 0 }

// spriteWidth and spriteHeight are sprite i's size on screen, doubled in
// each direction by its $D01D and $D017 expansion bits.
func (v *VICII) spriteWidth(i uint8) uint16 {
	if v.spriteExpandedX(i) {
		return 48
	}
	return 24
}

func (v *VICII) spriteHeight(i uint8) uint8 {
	if v.spriteExpandedY(i) {
		return 42
	}
	return 21
}

// spriteStartDot is the first dot of a line that sprite i covers. The X
// registers place a sprite by its leftmost dot, 24 dots left of the display
// window's first column.
//
// The wrap is written out rather than left to "% DotsPerLine". DotsPerLine
// is 504, not a power of two, and ARMv6-M has no divide instruction; the
// modulo only lowers to this single compare for as long as the compiler can
// still prove X is at most 0x1FF, which is not a property to hang the dot
// path's cost on.
func (v *VICII) spriteStartDot(i uint8) uint16 {
	start := 24 + v.spriteX[i]
	if start >= DotsPerLine {
		start -= DotsPerLine
	}
	return start
}

// latchSpriteShape performs sprite i's pointer and data fetches, the
// p-access and three s-accesses the VIC-II makes in article cycles 58+2i
// and 59+2i. The three bytes are one row of the sprite, chosen by the row
// counter the chip derives from how far into its Y band the sprite is.
func (v *VICII) latchSpriteShape(i uint8) {
	if !v.spriteUnderDMA(i) {
		return
	}
	screenBase := (uint16(v.memPointers) >> 4) & 0x0F << 10
	ptr := plaVICSpriteLoad(screenBase + 0x03F8 + uint16(i))
	addr := uint16(ptr)*64 + uint16(v.spriteRow[i])*3
	v.spriteShape[i][0] = plaVICSpriteLoad(addr)
	v.spriteShape[i][1] = plaVICSpriteLoad(addr + 1)
	v.spriteShape[i][2] = plaVICSpriteLoad(addr + 2)
	v.decodeSpriteRow(i)
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

// phi0high runs after the eighth dot of every 8-dot cycle: it performs a Bad
// Line's c-access (article cycles 15-54) and hands the bus to the CPU for
// Phi2.
// phi0highDisplay is phi0high for the display run, where the c-access
// range covers every slot, so only the Bad Line question is left.
func (v *VICII) phi0highDisplay() {
	if v.badLine {
		v.cycleCAccess()
	}
}

func (v *VICII) phi0high(slot uint16) {
	if slot >= 4 && slot <= 43 && v.badLine {
		v.cycleCAccess()
	}
}

// baWarningCycles is how long BA stays low before AEC follows it: the
// lead time the VIC gives the CPU to retire in-flight writes before taking
// the bus. Three is the longest run of consecutive writes a 6502 can
// perform, which is not a coincidence - see TestCPUIsOffTheBusBeforeAECDrops.
const baWarningCycles = 3

// AEC reports the Address Enable Control pin: true while the CPU still
// reaches the bus, false once the VIC has taken it. It is derived rather
// than stored because it is not independent state - it is the far end of
// the warning baLowCycles is counting - and a stored copy would only be
// another thing to keep in step. Nothing inside the machine senses it: the
// CPU is held by BA, three cycles earlier (see CPU.TickPhi2). It is here
// for front ends that want to show who owns the bus.
func (v *VICII) AEC() bool {
	return v.baLowCycles <= baWarningCycles
}

// cycleCAccess reads one character pointer + color entry from the video
// matrix into the current row's buffer, during a Bad Line (section 3.7.2).
func (v *VICII) cycleCAccess() {
	if v.baLowCycles <= baWarningCycles {
		// Late DMA. The CPU keeps the bus for the three cycles of warning
		// BA gives it to retire its writes, so a c-access landing inside
		// that window finds the VIC's D0-D7 still disconnected and cannot
		// see the video matrix. Only a badline started late enough gets
		// here; a normal one begins its warning at slot 1 and reads from
		// slot 4. CPU-bus-derived colour data is not modeled by this
		// renderer.
		v.videoMatrixColor[v.VMLI] = 0xFF
		return
	}
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

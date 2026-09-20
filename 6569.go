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

	// Bad Line Condition raster range (section 3.5 of the VIC Article).
	badLineRasterStart = 0x30
	badLineRasterEnd   = 0xF7

	leftComp38  = 55  // CSEL=0: 38 columns (article $1F)
	leftComp40  = 48  // CSEL=1: 40 columns (article $18)
	rightComp38 = 359 // CSEL=0: 38 columns (article $14F)
	rightComp40 = 368 // CSEL=1: 40 columns (article $158)

	// The three bus cycles those four dots fall in. The comparator has to
	// see every dot, but only these slots contain a dot it can match, so
	// stepLine decides once a line whether the dot path need ask at all.
	// leftComp40 and leftComp38 share a slot; the right pair does not.
	borderSlotLeft    = leftComp40 / DotsPerCycle
	borderSlotRight38 = rightComp38 / DotsPerCycle
	borderSlotRight40 = rightComp40 / DotsPerCycle

	// The interior of the display window: every slot between the first
	// border comparison and the second.
	displayFirstSlot = borderSlotLeft + 1
	displaySlotAfter = borderSlotRight38

	// The bus cycles the graphics sequencer can take up a g-access result
	// in. The g-access runs in slots 5 to 44 and the reload is one slot
	// behind it, so these are that window shifted by one.
	reloadFirstSlot = 6
	reloadSlotAfter = 46

	// vincSlot is the bus cycle carrying VINC, the VIC-II's own
	// increment-vertical-counter strobe, and so the bus cycle the raster
	// counter moves in. It is the article's cycle 1 - the start of a
	// raster line in the article's terms - which rebases onto slot 53.
	//
	// VINC is not where the beam wraps. The counter moves one cycle after
	// video is blanked and well before the beam reaches the left edge
	// again, so the retrace that follows belongs to the new line. Dot 0 is
	// the leftmost dot the beam paints, 80 dots after VINC.
	vincSlot = 53

	// RasterIncrementCycle is the same bus cycle counted the way a trace
	// counts them, from 1 rather than from 0. It is exported because a
	// trace's DOT and RASTER columns do not step together: RASTER moves on
	// VINC, nine cycles before the beam wraps.
	RasterIncrementCycle = vincSlot + 1

	// A dot no beam reaches, so a comparison against it never fires.
	noCompareDot = DotsPerLine + 1
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
	// in. It is the beam counter: dot follows from it, and the line ends
	// because the loop stopped rather than because a test caught it.
	slot       uint16 // 0 to 62
	dot        uint16 // 0 to 503
	rasterLine uint16 // 0 to 311

	// Keep per-dot and per-cycle scalar state before the larger buffers so
	// TinyGo can use compact fixed-offset accesses.
	//
	// lineVisible caches "rasterLine is outside vblank" for the current
	// raster line. lineDrawable further narrows that to the lines the
	// active pixel sink actually stores. Vertical blanking and the render
	// window are properties of the line, not of the dot, and rasterLine is
	// only ever written by endLine's line wrap, so every dot on a line
	// gives the same answer. Caching turns each dotclock's test into a
	// single byte load instead of reloading rasterLine and redoing range
	// compares - which LLVM cannot hoist for us, since the pixel sink call
	// may alias this struct and forces a reload after every paint. Kept in
	// sync by syncLineVisibility.
	lineVisible    bool
	lineDrawable   bool
	mainBorder     bool
	verticalBorder bool
	gdSequencer    uint8
	gdColor        [2]uint8
	borderColor    uint8
	background0    uint8
	control1       uint8
	control2       uint8
	memPointers    uint8

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

	// beamLine is the raster line the beam is on, and so which
	// framebuffer row a painted dot lands in. It steps where the beam
	// wraps, at the end of the line. (dot, beamLine) names a pixel.
	//
	// rasterLine, above, is a register rather than a position: the value
	// $D012 reads. It steps on VINC, in slot 53, because that is where the
	// VIC-II tells the monitor to recall its beam - an artifact of driving
	// a CRT, not a fact about where the picture starts.
	//
	// Every rule in this file keys off rasterLine rather than beamLine -
	// the Bad Line condition, the border unit's top and bottom
	// comparisons, VCBase's reload - because on the chip there is only one
	// counter, and it is the one the CPU reads. Programs sync on it and
	// then count cycles, which is what pins it to slot 53.
	//
	// The two differ only from VINC to the end of the line: slots 53 to
	// 62, dots 424 to 503, every one of them past VisibleDotsPerLine. So
	// they always agree wherever a pixel is written.
	// TestBeamLineAgreesWithRasterWherePainted pins that.
	//
	// It sits here, at the cold end of the scalars, so that adding it left
	// every field above at the offset it already had. Moving it up costs
	// more in the fields it displaces than it saves on its own two reads a
	// cycle.
	beamLine uint16 // 0 to 311

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
	v.slot = 0
	v.dot = 0
	v.rasterLine = 0
	v.BA = true
	v.AEC = true
	v.mainBorder = false
	v.verticalBorder = false
	v.gdSequencer = 0
	v.videoBuffer = 0
	v.gdColor = [2]uint8{v.background0, 0}
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
	v.syncLineVisibility()
}

// syncLineVisibility recomputes the cached line visibility flags from
// rasterLine, and puts the beam on the line the counter names.
// It must be called whenever rasterLine is changed by anything other than
// dotclock7's line wrap, which updates the flags itself.
func (v *VICII) syncLineVisibility() {
	v.beamLine = v.rasterLine
	v.lineVisible = v.rasterLine < firstVBlankLine && v.rasterLine > lastVBlankLine
	v.lineDrawable = v.rasterLine >= renderFirstLine && v.rasterLine < renderLineAfter
}

func (v *VICII) WriteRegister(addr uint16, value uint8) {
	reg := addr & 0x3F
	switch {
	case reg < 0x11:
		v.registers00To10[reg] = value
	case reg == regControl1:
		v.control1 = value
	case reg < regControl2:
		v.registers12To15[reg-0x12] = value
	case reg == regControl2:
		v.control2 = value
	case reg == 0x17:
		v.register17 = value
	case reg == regMemPointers:
		v.memPointers = value
	case reg < regBorderColor:
		v.registers19To1F[reg-0x19] = value
	case reg == regBorderColor:
		v.borderColor = value
	case reg == regBackground0:
		v.background0 = value
		v.gdColor[0] = value
	case reg < 0x2F:
		v.registers22To2E[reg-0x22] = value
	}
}

// ReadRegister reads a VIC-II register, mirrored every 64 bytes across
// $D000-$D3FF. Registers beyond $D02E don't exist and read as all 1 bits.
// $D012 (and bit 7 of $D011) are read-only mirrors of the live raster
// line, distinct from whatever was last written there (the raster compare
// target for IRQ generation, not yet implemented).
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
	case regControl2:
		return v.control2
	case regMemPointers:
		return v.memPointers
	case regBorderColor:
		return v.borderColor
	case regBackground0:
		return v.background0
	}
	switch {
	case reg < 0x11:
		return v.registers00To10[reg]
	// Unsigned offset tests prove both bounds, including the lower bounds
	// hidden by the special-register returns above.
	case reg-0x12 < uint16(len(v.registers12To15)):
		return v.registers12To15[reg-0x12]
	case reg == 0x17:
		return v.register17
	case reg-0x19 < uint16(len(v.registers19To1F)):
		return v.registers19To1F[reg-0x19]
	case reg-0x22 < uint16(len(v.registers22To2E)):
		return v.registers22To2E[reg-0x22]
	}
	return 0xFF
}

// StepFrame advances the VIC-II, and therefore the rest of the machine it
// clocks, by exactly one PAL frame: RasterLinesPerFrame raster lines.
//
// The frame is walked as lines, and a line as runs of bus cycles, because
// that is where the answers live. Whether a line paints at all is fixed for
// the line; which of its slots can carry a border comparison is fixed for
// every line there has ever been. Asking either per cycle - or worse, per
// dot - is asking a question whose answer was already settled.
func (v *VICII) StepFrame() {
	// Walking lines needs the beam parked on a line boundary. A caller
	// that has been stepping single cycles may leave it anywhere, so that
	// case falls back to stepping cycles.
	if v.slot != 0 {
		for range CyclesPerFrame {
			v.stepCycle()
		}
		return
	}
	for range RasterLinesPerFrame {
		v.stepLine()
	}
}

// advance moves the beam to the next bus cycle. There is no test here for
// running off the end of the line: stepLine knows where its run of slots
// stopped, and calls endLine once. What made that test look necessary was
// the raster counter moving at the end of the line, so the line's last
// cycle had to see it already moved; the counter moves on VINC now, nine
// cycles earlier, and nothing in a cycle reads the beam's row but the
// paint.
func (v *VICII) advance(slot uint16) {
	v.slot = slot + 1
	v.dot = v.slot * DotsPerCycle
}

// endLine wraps the beam to the start of the next row. The raster counter
// is not touched: it moved on VINC, in slot 53.
func (v *VICII) endLine() {
	v.slot = 0
	v.dot = 0
	v.beamLine++
	if v.beamLine >= RasterLinesPerFrame {
		v.beamLine = 0
	}
}

// stepLine advances the machine by one raster line: CyclesPerLine bus
// cycles, starting at slot 0.
//
// A line has more in common across it than a bus cycle does, and this is
// where that is spent. Which slots can match a border comparison is fixed
// for every line - the comparator's four dots fall in slots 6, 44 and 46
// and nowhere else - so the line is walked as runs with the answer built
// into each rather than asked 63 times.
//
// The runs are chosen by lineVisible, not lineDrawable. The vertical border
// flip-flop is moved by the comparator at raster lines the render window
// can crop away on a target with a smaller panel than the C64's picture,
// and a cropped line still has to move it. Painting is what drawability
// gates, and the paint path gates it per group.
func (v *VICII) stepLine() {
	if !v.lineVisible {
		v.blankRun(0, vincSlot)
		v.cycleBlankVINC(vincSlot)
		v.blankRun(vincSlot+1, CyclesPerLine)
		v.endLine()
		return
	}

	// The reload window opens at slot 6, which is also the first slot the
	// comparator can match, and closes after 45 - one slot before the
	// comparator's last. So the runs already divide on it.
	v.drawRun(0, borderSlotLeft, false)
	v.cycleDrawBorder(borderSlotLeft, true)
	v.drawDisplayRun()
	v.cycleDrawBorder(borderSlotRight38, true)
	v.drawRun(borderSlotRight38+1, borderSlotRight40, true)
	v.cycleDrawBorder(borderSlotRight40, false)
	v.drawRun(borderSlotRight40+1, vincSlot, false)
	v.cycleDrawVINC(vincSlot)
	v.drawRun(vincSlot+1, CyclesPerLine, false)
	v.endLine()
}

// drawRun paints the bus cycles from slot through to-1. None of them is a
// slot the border comparator can match, which is what makes it a run: the
// comparison is not merely hoisted out of the dot, it is gone.
func (v *VICII) drawRun(from, to uint16, mayReload bool) {
	for slot := from; slot < to; slot++ {
		v.cycleDraw(slot, mayReload)
	}
}

// drawDisplayRun paints the 37 bus cycles of the display window. Every
// slot test in phi0low and phi0high has a fixed answer across it, so the
// run gets half-phases with those answers built in.
func (v *VICII) drawDisplayRun() {
	for slot := uint16(displayFirstSlot); slot < displaySlotAfter; slot++ {
		v.cycleDrawDisplay(slot)
	}
}

// cycleDrawDisplay is cycleDraw for the interior of the display window.
// The body is spelled out rather than shared with cycleDraw for the reason
// cycleDraw gives: nothing in this path inlines, so a shared helper would
// be real calls - which is the cost this is removing in the first place.
func (v *VICII) cycleDrawDisplay(slot uint16) {
	dot := slot * DotsPerCycle
	// Every slot in the run is inside the reload window, so mayReload is
	// not a question here.
	v.commitGAccess()
	v.dotclock4(dot)
	v.phi0lowDisplay()
	v.dotclock4(dot + 4)
	// The run stops well before the end of the line, so the beam moves
	// without the wrap test advance carries.
	v.slot = slot + 1
	v.dot = v.slot * DotsPerCycle
	v.phi0highDisplay()
	cpu.TickPhi2()
	ciaTick()
	iecTick()
}

// blankRun runs the bus cycles from slot through to-1 without painting.
// Everything the VIC-II and the CPU do in a cycle still happens; only the
// beam reaches nothing.
func (v *VICII) blankRun(from, to uint16) {
	for slot := from; slot < to; slot++ {
		v.cycleBlank(slot)
	}
}

// cycleDraw runs one painting bus cycle in a slot the comparator cannot
// match. Its two half-phases are one call each, and the beam counter moves
// once here rather than eight times through the dot path.
func (v *VICII) cycleDraw(slot uint16, mayReload bool) {
	dot := slot * DotsPerCycle
	if mayReload {
		v.commitGAccess()
	}
	v.dotclock4(dot)
	v.phi0low(slot)
	v.dotclock4(dot + 4)
	v.advance(slot)
	v.phi0high(v.slot)
	cpu.TickPhi2()
	ciaTick()
	iecTick()
}

// cycleDrawBorder is cycleDraw for the three slots a line whose dots the
// border comparator can match. The comparison stays per dot - both values
// of a pair are a single dot, and the two pairs are one dot apart in phase,
// so no per-slot decode can reach all four - but which pair it matches
// against is asked once for the half-phase, in borderComparePair.
func (v *VICII) cycleDrawBorder(slot uint16, mayReload bool) {
	dot := slot * DotsPerCycle
	if mayReload {
		v.commitGAccess()
	}
	v.dotclockBorder4(dot)
	v.phi0low(slot)
	v.dotclockBorder4(dot + 4)
	v.advance(slot)
	v.phi0high(v.slot)
	cpu.TickPhi2()
	ciaTick()
	iecTick()
}

// cycleBlank runs one bus cycle whose dots reach nothing: the same
// sequence, with the beam stepping over the dots instead of shifting them
// out. Everything the VIC-II and the CPU do in a cycle still happens.
func (v *VICII) cycleBlank(slot uint16) {
	v.phi0low(slot)
	v.advance(slot)
	v.phi0high(v.slot)
	cpu.TickPhi2()
	ciaTick()
	iecTick()
}

// cycleDrawVINC is cycleDraw for the one slot a line carrying VINC. The
// slot is outside the reload window and cannot match the border
// comparator, so neither question is asked here either.
func (v *VICII) cycleDrawVINC(slot uint16) {
	dot := slot * DotsPerCycle
	v.dotclock4(dot)
	v.vinc()
	v.phi0low(slot)
	v.dotclock4(dot + 4)
	v.advance(slot)
	v.phi0high(v.slot)
	cpu.TickPhi2()
	ciaTick()
	iecTick()
}

// cycleBlankVINC is cycleBlank for that slot. A blanked line still has to
// move the counter - vblank is where most of them are.
func (v *VICII) cycleBlankVINC(slot uint16) {
	v.vinc()
	v.phi0low(slot)
	v.advance(slot)
	v.phi0high(v.slot)
	cpu.TickPhi2()
	ciaTick()
	iecTick()
}

// commitGAccess takes up the g-access result fetched one bus cycle earlier.
// It runs on the first dot of the slot, which is why it sits at the top of
// the cycle rather than inside the dot path.
func (v *VICII) commitGAccess() {
	v.gdSequencer = v.gdPending
	v.videoBuffer = v.videoBufferPending
	v.gdColor[1] = byte(v.videoBuffer >> 8)
}

// stepCycle advances the machine by one bus cycle, whichever slot it is in.
// StepFrame walks whole lines instead; this is what a caller stepping a
// cycle at a time gets.
func (v *VICII) stepCycle() {
	slot := v.slot
	borderSlot := slot == borderSlotLeft || slot == borderSlotRight38 ||
		slot == borderSlotRight40
	mayReload := slot >= reloadFirstSlot && slot < reloadSlotAfter
	switch {
	case !v.lineVisible && slot == vincSlot:
		v.cycleBlankVINC(slot)
	case !v.lineVisible:
		v.cycleBlank(slot)
	case slot == vincSlot:
		v.cycleDrawVINC(slot)
	case borderSlot:
		v.cycleDrawBorder(slot, mayReload)
	default:
		v.cycleDraw(slot, mayReload)
	}

	// stepLine wraps the line when its run of slots ends. Stepping one
	// cycle at a time, the last slot is where that falls.
	if slot == CyclesPerLine-1 {
		v.endLine()
	}
}

// StepCycle advances the singleton machine by exactly one bus cycle.
func (v *VICII) StepCycle() {
	v.stepCycle()
}

// StepFrame advances the singleton machine by exactly one PAL frame. See
// VICII.StepFrame.
func StepFrame() {
	vic.StepFrame()
}

// dotclock4 paints a Phi0 half-phase's four dots as one operation, in a
// slot where the border flip-flops cannot move. That is what lets the
// window test, the crop and the border state be read once for the group
// rather than four times: the answer cannot change underneath it.
func (v *VICII) dotclock4(dot uint16) {
	if !v.lineDrawable {
		return
	}
	if dot >= renderFirstDot && dot+3 < renderDotAfter {
		if v.verticalBorder {
			c := v.borderColor & 0x0F
			writePixels4ToBuffer(dot, v.beamLine, c, c, c, c)
			return
		}
		// The sequencer shifts once per dot, so the four dots read bits 7
		// to 4; shifting by four afterwards leaves it where four
		// single-dot shifts would.
		c0 := v.gdColor[v.gdSequencer>>7]
		c1 := v.gdColor[v.gdSequencer>>6&1]
		c2 := v.gdColor[v.gdSequencer>>5&1]
		c3 := v.gdColor[v.gdSequencer>>4&1]
		v.gdSequencer <<= 4
		if v.mainBorder {
			c := v.borderColor
			c0, c1, c2, c3 = c, c, c, c
		}
		writePixels4ToBuffer(dot, v.beamLine, c0&0x0F, c1&0x0F, c2&0x0F, c3&0x0F)
		return
	}
	// The group straddles the window edge, or is wholly outside it. The
	// sequencer still has to shift, because the dots happened.
	v.paintDotOutOfGroup(dot)
	v.paintDotOutOfGroup(dot + 1)
	v.paintDotOutOfGroup(dot + 2)
	v.paintDotOutOfGroup(dot + 3)
}

// dotclockBorder4 is dotclock4 for the three slots a line the comparator
// can match in. The comparator runs before the paint: the dot a comparison
// fires on is painted with the state it just set, not the one before.
//
// Which pair of dots CSEL selects is asked once here rather than per dot.
// The CPU only reaches $D016 between half-phases, so it cannot move under
// a group; asking it per half-phase rather than per cycle is what keeps a
// mid-cycle CSEL write reaching the comparison in the half after it.
func (v *VICII) dotclockBorder4(dot uint16) {
	left, right := v.borderComparePair()

	v.borderCompare(dot, left, right)
	v.paintDot(dot)
	v.borderCompare(dot+1, left, right)
	v.paintDot(dot + 1)
	v.borderCompare(dot+2, left, right)
	v.paintDot(dot + 2)
	v.borderCompare(dot+3, left, right)
	v.paintDot(dot + 3)
}

// borderCompare is section 3.9 rule 1: one comparator against the beam,
// with CSEL choosing which pair of values it matches.
func (v *VICII) borderCompare(dot, left, right uint16) {
	if dot == right {
		// "1. If the X coordinate reaches the right comparison value, the
		// main border flip flop is set."
		v.mainBorder = true
	} else if dot == left {
		v.resolveVerticalBorder()
	}
}

// resolveVerticalBorder is rules 4, 5 and 6: reached at the left
// comparison value, the vertical flip-flop answers to the raster line and
// the main flip-flop answers to it.
func (v *VICII) resolveVerticalBorder() {
	// "4./5. If the X coordinate reaches the left comparison value and the
	// Y coordinate reaches the bottom/top one, set/reset (if DEN) the
	// vertical border flip flop."
	rsel := (v.control1 >> 3) & 1
	if v.rasterLine == bottomComp[rsel] {
		v.verticalBorder = true
	}
	if v.rasterLine == topComp[rsel] && v.control1&0x10 != 0 {
		v.verticalBorder = false
	}
	// "6. If the X coordinate reaches the left comparison value and the
	// vertical border flip flop is not set, the main flip flop is reset."
	if !v.verticalBorder {
		v.mainBorder = false
	}
}

// borderComparePair is the pair of dots the comparator matches against,
// with CSEL choosing which width's pair that is. A line the beam cannot
// see matches neither, which is noCompareDot's whole job - a dot no beam
// reaches, so the per-dot comparison needs no visibility test of its own.
func (v *VICII) borderComparePair() (left, right uint16) {
	if !v.lineVisible {
		return noCompareDot, noCompareDot
	}
	if v.control2&csel != 0 {
		return leftComp40, rightComp40
	}
	return leftComp38, rightComp38
}

// paintDot decides and writes one dot's colour. Only the comparator's three
// slots a line want this; everywhere else the group holds for four dots and
// dotclock4 lifts the whole decision out.
func (v *VICII) paintDot(dot uint16) {
	if !v.lineDrawable {
		return
	}
	v.paintDotOutOfGroup(dot)
}

// paintDotOutOfGroup is paintDot with drawability already established: the
// per-dot path dotclock4 falls back to at the window's edge.
func (v *VICII) paintDotOutOfGroup(dot uint16) {
	if dot >= VisibleDotsPerLine {
		return
	}
	if v.verticalBorder {
		if dot >= renderFirstDot && dot < renderDotAfter {
			writePixelInWindow(dot, v.beamLine, v.borderColor&0x0F)
		}
		return
	}
	graphicsColor := v.gdColor[v.gdSequencer>>7]
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	if dot >= renderFirstDot && dot < renderDotAfter {
		writePixelInWindow(dot, v.beamLine, graphicsColor&0x0F)
	}
}

// phi0low runs on the first dot of every 8-dot cycle: while the VIC-II is
// in charge of the bus, it performs its own memory reads here (section
// 3.7.2 of the VIC Article). Sprites are not implemented yet.
//
// cycleRaster0/cycleRaster30/cycleIsBadLine/cycleIsCAccess are inlined
// directly here (each had exactly one call site, unconditional or nearly
// so) to remove function-call overhead from the frame fast path.
// phi0lowDisplay is phi0low for the display run, slots displayFirstSlot
// through displaySlotAfter-1. Every test in phi0low that asks which slot
// this is has the same answer across all 37 of them: VCBase reload (53),
// the DEN latch's end-of-line (52), the VC load (1-3), goto-idle (47) and
// the border comparison (52) are all outside the run, and the BA and
// g-access ranges cover all of it.
//
// It takes no slot: with every such test answered, nothing left in it
// depends on which of the 37 cycles this is.
//
// It must stay in step with phi0low; TestPhi0LowDisplayMatchesPhi0Low
// walks the run both ways and compares.
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

	v.BA = !badLine
	v.cycleGAccess()
}

func (v *VICII) phi0low(slot uint16) {
	// slot indexes the 8-dot bus cycles across a line. The article's cycle
	// numbering starts 10 slots later (its cycle N is our slot N-11, mod
	// 63), so the constants below are the article's rebased onto slot.

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
	// c-accesses, article cycles 12-54.
	v.BA = !(slot >= 1 && slot <= 43 && badLine)
	// g-access reads the video matrix entry stored by the c-access one
	// cycle earlier (c-access runs in the second phase of cycles 15-54;
	// g-access, in the first phase, can only see data from a strictly
	// earlier cycle), so its range is shifted one cycle later: 16-55.
	if slot >= 5 && slot <= 44 {
		v.cycleGAccess()
	}
}

// vinc moves the raster counter. It is article cycle 1 - the start of a
// raster line as the counter measures one - which is nine cycles before
// the beam wraps to the left edge. See vincSlot.
//
// Which slot carries it is known where the line is walked, so this is a
// slot of the run rather than a test in phi0low: every cycle of every line
// was paying for a question whose answer stepLine already had.
func (v *VICII) vinc() {
	v.rasterLine++
	if v.rasterLine >= RasterLinesPerFrame {
		v.rasterLine = 0
	}

	// The only place rasterLine moves, so the only place the cached
	// answers can go stale. beamLine is deliberately not touched: the beam
	// has not moved, and will not until the end of the line.
	v.lineVisible = v.rasterLine < firstVBlankLine && v.rasterLine > lastVBlankLine
	v.lineDrawable = v.rasterLine >= renderFirstLine && v.rasterLine < renderLineAfter

	// cycleRaster0: resets VCBase at the start of raster line 0, on the
	// VINC that begins it.
	if v.rasterLine == 0 {
		v.VCBase = 0
	}
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

// cycleGAccess reads one row of character data (standard text mode only
// for now), and advances VC/VMLI (section 3.7.2/3.7.3.1). The fetched
// byte isn't displayed immediately: it's latched in gdPending and
// committed to gdSequencer/videoBuffer when dotclock7 advances the beam to
// the next character-cell boundary, 4 dots after this cycle's own dot&7==4.
func (v *VICII) cycleGAccess() {
	v.videoBufferPending = v.videoMatrixColor[v.VMLI]

	cb := (uint16(v.memPointers) >> 1) & 0x07
	addr := (cb << 11) + (v.videoBufferPending&0xFF)<<3 + uint16(v.RC)
	v.gdPending = plaVICLoad(addr)

	// "VC and VMLI are incremented after each g-access in display state"
	// (section 3.7.2, rule 4): idle state g-accesses don't advance them.
	if !v.idle {
		v.VC++
		v.VMLI++
	}
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
// phi0highDisplay is phi0high for the display run, where the c-access
// range covers every slot, so only the Bad Line question is left.
//
// It must stay in step with phi0high; TestPhi0HighDisplayMatchesPhi0High
// walks the run both ways and compares.
func (v *VICII) phi0highDisplay() {
	if v.badLine {
		v.cycleCAccess()
	}
	v.AEC = v.BA
}

func (v *VICII) phi0high(slot uint16) {
	// slot as in phi0low, but 4 dots later, so its offset from the
	// article's cycle numbering differs by one, which is why the caller
	// passes slot+1.
	if slot >= 5 && slot <= 44 {
		v.cycleCAccess()
	}

	// AEC mirrors BA with a delay, or is directly controlled here
	v.AEC = v.BA
}

// cycleCAccess reads one character pointer + color entry from the video
// matrix into the current row's buffer, during a Bad Line (section 3.7.2).
func (v *VICII) cycleCAccess() {
	if !v.badLine {
		return
	}
	vm := (uint16(v.memPointers) >> 4) & 0x0F
	char := plaVICLoad((vm << 10) + v.VC)
	// Colour RAM is a dedicated 2114 chip wired directly to the VIC-II's
	// colour bus, not part of the 64K address space the c-access above
	// reads through, so it is read here independently of plaVICLoad.
	color := colorRAM[v.VC]
	v.videoMatrixColor[v.VMLI] = uint16(color)<<8 | uint16(char)
}

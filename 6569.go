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
	// one dot right per dot clock. The remainder of the line is the
	// horizontal blanking interval, during which the beam retraces to the
	// left and the line counter advances.
	//
	// This is a rendering window, not a timing constant. It gates nothing
	// but calls to WritePixelToBuffer: the beam still ticks DotsPerFrame
	// (157,248) times per frame regardless, and every state transition is
	// driven by dot and rasterLine against their own comparators - the
	// border flip-flops by leftComp/rightComp, the g-access commit by its
	// slot range, Bad Lines by rasterLine. Widening or narrowing it cannot
	// change emulated behaviour, only which border pixels a frontend is
	// handed.
	//
	// So it is chosen for convenience. A bus cycle starts at a dot
	// divisible by DotsPerCycle and paints the next eight, the last of
	// them by dotclock0, which stepCycle always calls. Making this
	// congruent to 1 modulo DotsPerCycle means the seven dots dotclock1
	// through dotclock7 paint are always either all inside the window or
	// all outside it, never split, which is what lets stepCycle test
	// horizontal blanking once per cycle instead of once per dot.
	//
	// The VIC Article does not settle on one figure anyway: rebasing its
	// section 3.9 table, whose X=480 is our dot 0 and whose last visible
	// X=380 is our dot 404, suggests 405, while its own summary column
	// says 403. 401 sits inside that range, and the dots it gives up are
	// right border - the 40-column window ends at rightComp40 (368) and
	// sprites are unimplemented, so only flat borderColor can appear
	// beyond it.
	VisibleDotsPerLine = 401

	// VisibleDotsPerLine must stay congruent to 1 modulo DotsPerCycle, or
	// a cycle's seven dots could straddle the edge of the window and
	// stepCycle's single test would be wrong. These conversions are valid
	// constant expressions only when the remainder is exactly 1, so
	// changing it without fixing stepCycle fails the build.
	_ = uint(VisibleDotsPerLine%DotsPerCycle - 1)
	_ = uint(1 - VisibleDotsPerLine%DotsPerCycle)

	// PAL 6569 vertical blanking interval: raster lines 300-311 and 0-15,
	// during which the video signal (and thus the raster) is off.
	firstVBlankLine = 300
	lastVBlankLine  = 15

	// The picture occupies raster lines 16-299, and horizontally dots
	// 0..VisibleDotsPerLine-1; writePixelToBuffer is never called outside
	// that window, so a display only needs a buffer that size.
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
	// only ever written by dotclock0's line wrap, so every dot on a line
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
	v.dot = 0
	v.rasterLine = 0
	v.BA = true
	v.AEC = true
	v.mainBorder = false
	v.verticalBorder = false
	v.gdSequencer = 0
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
	v.syncLineVisibility()
}

// syncLineVisibility recomputes the cached line visibility flags from rasterLine.
// It must be called whenever rasterLine is changed by anything other than
// dotclock0's line wrap, which updates the flags itself.
func (v *VICII) syncLineVisibility() {
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

func (v *VICII) StepDot() {
	// Compute the next pixel with the implementation for its phase. The
	// phase is derived from the current beam position so StepDot remains
	// correct when called from any point within a bus cycle.
	//
	// dotclock1 through dotclock7 carry no visibility test of their own:
	// their caller owns it, because stepCycle can answer it once for all
	// seven at a time. Anything driving them one at a time must therefore
	// test it themselves. dotclock0 still tests itself, because its
	// line wrap is what changes the answer. When the dot is outside the
	// rendering window there is nothing to paint, so the beam just
	// advances.
	if (v.lineVisible && v.dot+1 < VisibleDotsPerLine) || v.dot&7 == 7 {
		switch v.dot & 7 {
		case 0:
			v.dotclock1()
		case 1:
			v.dotclock2()
		case 2:
			v.dotclock3()
		case 3:
			v.dotclock4()
		case 4:
			v.dotclock5()
		case 5:
			v.dotclock6()
		case 6:
			v.dotclock7()
		case 7:
			v.dotclock0()
		}
	} else {
		v.dot++
	}

	// Every 8 dots represents 1 full CPU cycle (Phi1 + Phi2). The
	// cycle boundary sits 4 dots off dot=0 (the chip's bus cycles aren't
	// phase-aligned to the left edge of the picture), so phi0low lands on
	// dot&7==4 and phi0high, 4 dots later, on the next dot&7==0.
	switch v.dot & 7 {
	case 4:
		v.phi0low()
	case 0:
		v.phi0high()
		cpu.TickPhi2()
		iecTick()
	}
}

// StepFrame advances the VIC-II, and therefore the rest of the machine it
// clocks, by exactly one PAL frame: CyclesPerFrame bus cycles, each of
// which is stepCycle's DotsPerCycle dots. This is a relative step: if
// StepFrame is interleaved with StepDot, it preserves the current beam
// phase and lands one frame later at the same dot/raster position rather
// than synchronizing to the next frame boundary.
//
// It assumes the beam sits on a bus-cycle boundary (dot divisible by
// DotsPerCycle) on entry, which is where Reset and FinishFrame both leave
// it. Callers that interleave StepDot must return to a cycle boundary
// before using this optimized frame path.
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
// on the 8th (see StepDot).
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
// Both regressions trace back to repeatedly entering the general per-dot
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
	// One test covers the seven dots this cycle paints before dotclock0.
	// Whether the line is drawable is a property of the line, cached in
	// lineDrawable. Whether the dots are inside the horizontal window
	// would normally be a per-dot question, but dot is always a multiple
	// of DotsPerCycle and VisibleDotsPerLine is congruent to 1 modulo it
	// (asserted where it is declared), so dots dot+1 through dot+7 are
	// either all inside the window or all outside it, never split.
	// dotclock1 through dotclock7 therefore carry no hblank check at all.
	inWindow := v.dot+1 < VisibleDotsPerLine
	paint := v.lineDrawable && inWindow
	if paint {
		v.dotclock1()
		v.dotclock2()
		v.dotclock3()
		v.dotclock4()
	} else {
		v.dot += 4
	}
	v.phi0low()
	if paint {
		v.dotclock5()
		v.dotclock6()
		v.dotclock7()
	} else if v.lineVisible && inWindow {
		// dotclock7 carries the CSEL=0 border comparators (leftComp38 55,
		// rightComp38 359), so it must still run on lines that are visible
		// but not drawable, or the border flip-flops would miss a
		// transition. Outside the horizontal window it can be skipped
		// too: both comparators are below VisibleDotsPerLine, so a cycle
		// starting at dot 400 or later cannot reach either.
		v.dot += 2
		v.dotclock7()
	} else {
		v.dot += 3
	}
	// dotclock0 always runs, on blanked lines too: it owns the line wrap,
	// and the dot it paints is the first of the new line, which may have
	// just become visible, so paint above cannot speak for it. Keeping it
	// outside the branch also leaves it, phi0low, phi0high and TickPhi2
	// with exactly one call site each.
	v.dotclock0()
	v.phi0high()
	cpu.TickPhi2()
	iecTick()
}

// FinishFrame advances the VIC-II, and therefore the rest of the machine it
// clocks, until the beam reaches the top of the next frame. Unlike StepFrame,
// this synchronizes to dot 0, raster line 0; if already at that position, it
// still advances one full frame.
func (v *VICII) FinishFrame() {
	for {
		v.StepDot()
		if v.dot == 0 && v.rasterLine == 0 {
			return
		}
	}
}

// StepFrame advances the singleton machine by exactly one PAL frame. See
// VICII.StepFrame for the behaviour when interleaving it with lower-level
// StepDot calls.
func StepFrame() {
	vic.StepFrame()
}

// FinishFrame advances the singleton machine to dot 0, raster line 0 at the
// top of the next frame. See VICII.FinishFrame for boundary behaviour.
func FinishFrame() {
	vic.FinishFrame()
}

// dotclock1 through dotclock6 paint the interior dots of a bus cycle
// (those that immediately follow phi0low's dot but precede phi0high's -
// see stepCycle). Every check beyond the pixel paint itself only ever
// triggers on one specific dot within a cycle:
//   - the line/frame wrap only happens advancing off dot 503 (the last
//     dot of a line, DotsPerLine-1), which only the 8th dot of a cycle
//     (dotclock0) can reach, since DotsPerLine is a multiple of
//     DotsPerCycle;
//   - the border comparisons only match dots 48, 55, 359, and 368 (see
//     leftComp/rightComp), all of which are ≡ 7 or 0 (mod 8) - border
//     transitions only ever land on a character-cell boundary;
//   - the g-access commit only runs when dot&7==0, true by definition
//     only for the 8th dot.
//
// So these six functions are byte-for-byte identical to each other: just
// the beam advance and the pixel paint. They carry no visibility test at
// all; stepCycle establishes that for all 8 dots of a cycle at once.
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
func (v *VICII) dotclock1() {
	v.dot++

	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
}

// dotclock2 is dotclock1 for the cycle's 2nd dot - see dotclock1's comment.
func (v *VICII) dotclock2() {
	v.dot++

	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
}

// dotclock3 is dotclock1 for the cycle's 3rd dot - see dotclock1's comment.
func (v *VICII) dotclock3() {
	v.dot++

	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
}

// dotclock4 is dotclock1 for the cycle's 4th dot - see dotclock1's
// comment. phi0low runs immediately after this call (see stepCycle).
func (v *VICII) dotclock4() {
	v.dot++

	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
}

// dotclock5 is dotclock1 for the cycle's 5th dot - see dotclock1's comment.
func (v *VICII) dotclock5() {
	v.dot++

	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
}

// dotclock6 is dotclock1 for the cycle's 6th dot - see dotclock1's comment.
func (v *VICII) dotclock6() {
	v.dot++

	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
}

// dotclock7 handles the cycle's 7th dot, the first of the two dots that
// can carry a border transition (see dotclock1's comment): dot values
// ≡7 (mod 8) are the only ones that can equal rightComp[0] (359) or
// leftComp[0] (55), so this only needs to check those two, not the
// switch over all four comparison values. Like dotclock1
// through dotclock6, the line/frame wrap and the g-access commit can't
// trigger here, so they're omitted too.
func (v *VICII) dotclock7() {
	v.dot++

	if v.dot == rightComp38 && v.control2&csel == 0 {
		// "1. If the X coordinate reaches the right comparison value, the
		// main border flip flop is set."
		v.mainBorder = true
	}
	if v.dot == leftComp38 && v.control2&csel == 0 {
		// "4./5. If the X coordinate reaches the left comparison value
		// and the Y coordinate reaches the bottom/top one, set/reset
		// (if DEN) the vertical border flip flop."
		rsel := (v.control1 >> 3) & 1
		if v.rasterLine == bottomComp[rsel] {
			v.verticalBorder = true
		}
		if v.rasterLine == topComp[rsel] && v.control1&0x10 != 0 {
			v.verticalBorder = false
		}
		// "6. If the X coordinate reaches the left comparison value and
		// the vertical border flip flop is not set, the main flip flop
		// is reset."
		if !v.verticalBorder {
			v.mainBorder = false
		}
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
}

// dotclock0 handles the cycle's 8th (and a line's or frame's last) dot:
// the only dot that can push the beam off the end of a line (and, once
// every RasterLinesPerFrame lines, off the end of a frame too), the only
// one that can equal rightComp[1] (368) or leftComp[1] (48) - the other
// half of the border comparisons dotclock7 doesn't check - and, since
// dot&7==0 is true here by definition, the one that always evaluates the
// g-access commit.
func (v *VICII) dotclock0() {
	v.dot++
	if v.dot >= DotsPerLine {
		v.dot = 0
		v.rasterLine++
		if v.rasterLine >= RasterLinesPerFrame {
			v.rasterLine = 0
		}
		// The only place rasterLine changes in the hot path, so the only
		// place the cached visibility answers can go stale.
		v.lineVisible = v.rasterLine < firstVBlankLine && v.rasterLine > lastVBlankLine
		v.lineDrawable = v.rasterLine >= renderFirstLine && v.rasterLine < renderLineAfter
	}

	if !v.lineVisible {
		return
	}
	if v.dot >= VisibleDotsPerLine {
		return
	}

	if v.dot == rightComp40 && v.control2&csel != 0 {
		v.mainBorder = true
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

	// dot&7==0 is always true here, so only the slot range needs checking.
	if slot := v.dot / 8; slot >= 6 && slot <= 45 {
		v.gdSequencer = v.gdPending
		v.videoBuffer = v.videoBufferPending
	}
	if !v.lineDrawable || v.dot < renderFirstDot || v.dot >= renderDotAfter {
		return
	}

	if v.verticalBorder {
		writePixelToBuffer(v.dot, v.rasterLine, v.borderColor&0x0F)
		return
	}
	var graphicsColor byte
	if v.gdSequencer&0x80 == 0 {
		graphicsColor = v.background0
	} else {
		graphicsColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
	if v.mainBorder {
		graphicsColor = v.borderColor
	}
	writePixelToBuffer(v.dot, v.rasterLine, graphicsColor&0x0F)
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
// committed to gdSequencer/videoBuffer by dotclock0 on the next
// character-cell boundary, 4 dots after this cycle's own dot&7==4.
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
func (v *VICII) phi0high() {
	// slot as in phi0low, but 4 dots later, so its offset from the
	// article's cycle numbering differs by one.
	slot := v.dot / 8
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

package tiny64

const (
	DotsPerLine         = 63 * 8 // PAL: 63 cycles * 8 dots
	RasterLinesPerFrame = 312    // PAL total raster lines

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

	// The picture occupies raster lines 16-299; WritePixelToBuffer is
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

	// Bad Line Condition raster range (section 3.5 of the VIC Article) and
	// the number of CPU cycles per raster line.
	badLineRasterStart = 0x30
	badLineRasterEnd   = 0xF7
	maxCyclesPerLine   = 63
)

// Border unit comparison values, indexed by the RSEL/CSEL control bits.
// Border/display window sizes can be switched mid-frame on real hardware.
// The X values are the VIC Article's section 3.9 coordinates rebased onto
// dot (its X=480 is our dot 0), so they can be compared against dot with
// no conversion; the Y values are raster lines and need none.
var (
	leftComp   = [2]uint16{55, 48}   // CSEL: 38 vs 40 columns (article $1F/$18)
	rightComp  = [2]uint16{359, 368} // CSEL: 38 vs 40 columns (article $14F/$158)
	topComp    = [2]uint16{0x37, 0x33}
	bottomComp = [2]uint16{0xF7, 0xFB}
)

type VICII struct {
	dot        uint16 // 0 to 503
	rasterLine uint16 // 0 to 311

	// Signals driven by the VIC-II and sensed by the CPU
	BA  bool // Bus Available (true = high/free, false = low/stalled)
	AEC bool // Address Enable Control (true = CPU owns Phi2, false = VIC owns Phi2)

	// VIC-II Internal Registers
	registers [47]uint8 // d000 to d02e

	// Border unit flip-flops (section 3.9 of the VIC Article): mainBorder
	// gates the whole display, verticalBorder additionally gates graphics.
	mainBorder     bool
	verticalBorder bool

	// grColor is the most recently computed graphics color (section 3.7.3).
	grColor byte
	// gdSequencer is the graphics data shift register: reloaded by a
	// g-access and shifted once per pixel.
	gdSequencer uint8
	// videoBuffer holds the c-access result (char code + color) consumed by
	// the most recent g-access.
	videoBuffer uint16
	// videoMatrixColor buffers one text row's worth of c-access results
	// (char code + color), indexed by VMLI.
	videoMatrixColor [40]uint16
	// gdPending/videoBufferPending hold a g-access's fetch result until it's
	// committed to gdSequencer/videoBuffer 4 dots later (see cycleGAccess).
	gdPending          uint8
	videoBufferPending uint16

	// VC/VCBase/VMLI/RC drive the video matrix and character row fetch
	// (section 3.7.2 of the VIC Article).
	VC     uint16
	VCBase uint16
	VMLI   uint8
	RC     uint8

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

	WritePixelToBuffer func(x, y uint16, colorIndex byte)
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
	v.grColor = 0
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
}

func (v *VICII) WriteRegister(addr uint16, value uint8) {
	reg := addr & 0x3F
	if int(reg) < len(v.registers) {
		v.registers[reg] = value
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
		val := v.registers[0x11] & 0x7F
		if v.rasterLine&0x100 != 0 {
			val |= 0x80
		}
		return val
	}
	if int(reg) < len(v.registers) {
		return v.registers[reg]
	}
	return 0xFF
}

func (v *VICII) StepDot() {
	// 1. Compute the pixel at the beam's new position.
	v.dotclock()

	// 2. Every 8 dots represents 1 full CPU cycle (Phi1 + Phi2). The
	// cycle boundary sits 4 dots off dot=0 (the chip's bus cycles aren't
	// phase-aligned to the left edge of the picture), so phi0low lands on
	// dot&7==4 and phi0high, 4 dots later, on the next dot&7==0.
	switch v.dot & 7 {
	case 4:
		v.phi0low()
	case 0:
		v.phi0high()
		cpu.TickPhi2()
	}
}

// dotclock paints the pixel at the beam's current position.
//
// borderUnit/display/calculateGraphicsInfo/cycleGAccessCommit are inlined
// directly here (each had exactly one call site, from this function) to
// remove per-dot function-call overhead - Cortex-M0+ has no branch
// prediction or return-address stack, so on that target a BL/BX pair plus
// prologue/epilogue is not free, and TinyGo's default (size-optimized)
// build doesn't inline this aggressively on its own.
func (v *VICII) dotclock() {
	// 1. Advance the video beam by exactly 1 pixel/dot
	v.dot++
	if v.dot >= DotsPerLine {
		v.dot = 0
		v.rasterLine++
		if v.rasterLine >= RasterLinesPerFrame {
			v.rasterLine = 0
		}
	}

	// are we in vertical blancking interval ?
	if v.rasterLine >= firstVBlankLine || v.rasterLine <= lastVBlankLine {
		return
	}

	// are we in horizontal blancking interval ?
	if v.dot >= VisibleDotsPerLine {
		return
	}

	// borderUnit (section 3.9 of the VIC Article): dot can only ever match
	// a comparison value at 4 fixed positions per line, so registers are
	// only read there instead of on every dot.
	switch v.dot {
	case rightComp[0], rightComp[1]:
		// "1. If the X coordinate reaches the right comparison value, the
		// main border flip flop is set."
		csel := (v.registers[regControl2] >> 3) & 1
		if v.dot == rightComp[csel] {
			v.mainBorder = true
		}
	case leftComp[0], leftComp[1]:
		csel := (v.registers[regControl2] >> 3) & 1
		if v.dot == leftComp[csel] {
			// "4./5. If the X coordinate reaches the left comparison value
			// and the Y coordinate reaches the bottom/top one, set/reset
			// (if DEN) the vertical border flip flop."
			rsel := (v.registers[regControl1] >> 3) & 1
			if v.rasterLine == bottomComp[rsel] {
				v.verticalBorder = true
			}
			if v.rasterLine == topComp[rsel] && v.registers[regControl1]&0x10 != 0 {
				v.verticalBorder = false
			}
			// "6. If the X coordinate reaches the left comparison value and
			// the vertical border flip flop is not set, the main flip flop
			// is reset."
			if !v.verticalBorder {
				v.mainBorder = false
			}
		}
	}

	// The byte fetched by this cycle's g-access becomes visible on the
	// character-cell boundary: committing it in phi0high would run after
	// this same dot's pixel is painted below, one pixel late.
	if v.dot&7 == 0 {
		if slot := v.dot / 8; slot >= 6 && slot <= 45 {
			v.gdSequencer = v.gdPending
			v.videoBuffer = v.videoBufferPending
		}
	}

	// display()/calculateGraphicsInfo(): border color while either border
	// flip-flop is set, otherwise the graphics color from the sequencer
	// (section 3.9/3.7.3.1, standard text mode only for now). Sprites are
	// not implemented yet.
	var colorIndex byte
	if v.verticalBorder {
		colorIndex = v.registers[regBorderColor] & 0x0F
	} else {
		if v.gdSequencer&0x80 == 0 {
			v.grColor = v.registers[regBackground0] // background color 0
		} else {
			v.grColor = byte(v.videoBuffer >> 8) // foreground color nibble
		}
		v.gdSequencer <<= 1 // this pixel is now shifted out

		if v.mainBorder {
			colorIndex = v.registers[regBorderColor] & 0x0F
		} else {
			colorIndex = v.grColor & 0x0F
		}
	}
	v.WritePixelToBuffer(v.dot, v.rasterLine, colorIndex)
}

// phi0low runs on the first dot of every 8-dot cycle: while the VIC-II is
// in charge of the bus, it performs its own memory reads here (section
// 3.7.2 of the VIC Article). Sprites are not implemented yet.
//
// cycleRaster0/cycleRaster30/cycleIsBadLine/cycleIsCAccess are inlined
// directly here (each had exactly one call site, unconditional or nearly
// so) for the same function-call-overhead reason as dotclock, above.
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
		if v.registers[regControl1]&0x10 != 0 {
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
	yscroll := v.registers[regControl1] & 0x07
	v.badLine = v.rasterLine >= badLineRasterStart && v.rasterLine <= badLineRasterEnd &&
		uint8(v.rasterLine)&0x07 == yscroll && v.allowBadLine
	// "The transition from idle to display state occurs as soon as there
	// is a Bad Line Condition" (section 3.7.1).
	if v.badLine {
		v.idle = false
	}

	v.BA = true // default; a Bad Line's c-access window pulls it low below

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
	if slot >= 1 && slot <= 43 && v.badLine {
		v.BA = false
	}
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
// committed to gdSequencer/videoBuffer by dotclock on the next
// character-cell boundary, 4 dots after this cycle's own dot&7==4.
func (v *VICII) cycleGAccess() {
	v.videoBufferPending = v.videoMatrixColor[v.VMLI]

	cb := (uint16(v.registers[regMemPointers]) >> 1) & 0x07
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
	rsel := (v.registers[regControl1] >> 3) & 1
	if v.rasterLine == bottomComp[rsel] {
		v.verticalBorder = true
	}
	if v.rasterLine == topComp[rsel] && v.registers[regControl1]&0x10 != 0 {
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
	vm := (uint16(v.registers[regMemPointers]) >> 4) & 0x0F
	char := plaVICLoad((vm << 10) + v.VC)
	color := ram[0xD800+v.VC] & 0x0F
	v.videoMatrixColor[v.VMLI] = uint16(color)<<8 | uint16(char)
}

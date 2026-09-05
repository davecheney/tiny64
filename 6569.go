package tiny64

const (
	DotsPerLine         = 63 * 8 // PAL: 63 cycles * 8 dots
	RasterLinesPerFrame = 312    // PAL total raster lines

	// PAL 6569 vertical blanking interval: raster lines 300-311 and 0-15,
	// during which the video signal (and thus the raster) is off.
	firstVBlankLine = 300
	lastVBlankLine  = 15

	// PAL 6569 horizontal blanking interval: dots 381-479 of each line,
	// during which the video signal (and thus the raster) is off.
	firstHBlankDot = 381
	lastHBlankDot  = 479

	// Real hardware X coordinate at the start of a raster line (i.e. where
	// Dot=0 falls): raster lines begin at X=404, not X=0.
	firstXCoo = 404

	// Real hardware X coordinate of the first visible pixel of a line. The
	// visible region wraps past the X=503/X=0 boundary (480-503, 0-380), so
	// this is used to remap rasterX to a contiguous display column.
	firstVisXCoo = 480

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
var (
	leftComp   = [2]int16{0x1F, 0x18}   // CSEL: 38 vs 40 columns
	rightComp  = [2]int16{0x14F, 0x158} // CSEL: 38 vs 40 columns
	topComp    = [2]int16{0x37, 0x33}   // RSEL: 24 vs 25 rows
	bottomComp = [2]int16{0xF7, 0xFB}   // RSEL: 24 vs 25 rows
)

type VICII struct {
	Dot        int16 // 0 to 503
	RasterLine int16 // 0 to 311

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

	WritePixelToBuffer func(x, y int, colorIndex byte)
}

var vic VICII

func VIC() *VICII {
	return &vic
}

// Reset restores the VIC-II's internal video logic state (beam position,
// border flip-flops, VC/VCBase/VMLI/RC, and idle/display state) to their
// power-on values. Registers ($D000-$D02E) are left untouched: real
// hardware doesn't clear them on RESET, KERNAL's IOINIT does that.
func (v *VICII) Reset() {
	v.Dot = 0
	v.RasterLine = 0
	v.BA = true
	v.AEC = true
	v.mainBorder = false
	v.verticalBorder = false
	v.grColor = 0
	v.gdSequencer = 0
	v.videoBuffer = 0
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
		return uint8(v.RasterLine)
	case 0x11:
		val := v.registers[0x11] & 0x7F
		if v.RasterLine&0x100 != 0 {
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

	// 2. Every 8 dots represents 1 full CPU cycle (Phi1 + Phi2).
	switch v.Dot % 8 {
	case 0:
		v.phi0low()
	case 4:
		v.phi0high()
		cpu.TickPhi2()
	}
}

// dotclock paints the pixel at the beam's current position.
func (v *VICII) dotclock() {
	// 1. Advance the video beam by exactly 1 pixel/dot
	v.Dot++
	if v.Dot >= DotsPerLine {
		v.Dot = 0
		v.RasterLine++
		if v.RasterLine >= RasterLinesPerFrame {
			v.RasterLine = 0
		}
	}

	// are we in vertical blancking interval ?
	if v.RasterLine >= firstVBlankLine || v.RasterLine <= lastVBlankLine {
		return
	}

	// rasterX is the real hardware X coordinate: our Dot counter resets to
	// 0 at the point hardware calls X=404, not X=0.
	rasterX := (v.Dot + firstXCoo) % DotsPerLine

	// are we in horizontal blancking interval ?
	if rasterX >= firstHBlankDot && rasterX <= lastHBlankDot {
		return
	}

	v.borderUnit(rasterX)

	// Remap to a contiguous display column: the visible region wraps past
	// the X=503/X=0 boundary, but the pixel buffer expects 0..N left-to-right.
	displayX := (rasterX - firstVisXCoo + DotsPerLine) % DotsPerLine
	v.WritePixelToBuffer(int(displayX), int(v.RasterLine), v.display())
}

// display chooses the color for the current dot: border color while either
// border flip-flop is set, otherwise the graphics color. Sprites are not
// implemented yet (section 3.9 of the VIC Article, minus the sprite/
// multiplexer steps).
func (v *VICII) display() byte {
	if v.verticalBorder {
		return v.registers[regBorderColor] & 0x0F
	}

	v.calculateGraphicsInfo()

	if v.mainBorder {
		return v.registers[regBorderColor] & 0x0F
	}
	return v.grColor & 0x0F
}

// calculateGraphicsInfo computes the graphics color for the current pixel
// from the graphics data sequencer (section 3.7.3.1, standard text mode
// only for now).
func (v *VICII) calculateGraphicsInfo() {
	if v.gdSequencer&0x80 == 0 {
		v.grColor = v.registers[regBackground0] // background color 0
	} else {
		v.grColor = byte(v.videoBuffer >> 8) // foreground color nibble
	}
	v.gdSequencer <<= 1 // this pixel is now shifted out
}

// borderUnit updates the mainBorder/verticalBorder flip-flops for the
// current dot, per section 3.9 of the VIC Article.
func (v *VICII) borderUnit(rasterX int16) {
	csel := (v.registers[regControl2] >> 3) & 1
	rsel := (v.registers[regControl1] >> 3) & 1
	den := v.registers[regControl1]&0x10 != 0

	// "1. If the X coordinate reaches the right comparison value, the main
	// border flip flop is set."
	if rasterX == rightComp[csel] {
		v.mainBorder = true
	}

	if rasterX == leftComp[csel] {
		// "4./5. If the X coordinate reaches the left comparison value and
		// the Y coordinate reaches the bottom/top one, set/reset (if DEN)
		// the vertical border flip flop."
		if v.RasterLine == bottomComp[rsel] {
			v.verticalBorder = true
		}
		if v.RasterLine == topComp[rsel] && den {
			v.verticalBorder = false
		}
		// "6. If the X coordinate reaches the left comparison value and the
		// vertical border flip flop is not set, the main flip flop is reset."
		if !v.verticalBorder {
			v.mainBorder = false
		}
	}
}

// phi0low runs on the first dot of every 8-dot cycle: while the VIC-II is
// in charge of the bus, it performs its own memory reads here (section
// 3.7.2 of the VIC Article). Sprites are not implemented yet.
func (v *VICII) phi0low() {
	cycle := int(v.Dot)/8 + 1

	v.cycleRaster0()
	v.cycleRaster30()
	v.cycleIsBadLine()

	v.BA = true // default; a Bad Line's c-access window pulls it low below

	switch cycle {
	case 12, 13, 14:
		v.cycleSetVicCounter()
	case 58:
		v.cycleGotoIdle()
	case 63:
		v.cycleBorderComp()
	}

	if cycle >= 12 && cycle <= 54 {
		v.cycleIsCAccess()
	}
	// g-access reads the video matrix entry stored by the c-access one
	// cycle earlier (c-access runs in the second phase of cycles 15-54;
	// g-access, in the first phase, can only see data from a strictly
	// earlier cycle), so its range is shifted one cycle later: 16-55.
	if cycle >= 16 && cycle <= 55 {
		v.cycleGAccess()
	}
}

// cycleRaster0 resets VCBase at the start of raster line 0 (section 3.7.2).
func (v *VICII) cycleRaster0() {
	if v.RasterLine == 0 && int(v.Dot)/8+1 == 1 {
		v.VCBase = 0
	}
}

// cycleRaster30 latches whether DEN was set at any point during raster
// line $30 into allowBadLine, at the end of that line (section 3.5).
func (v *VICII) cycleRaster30() {
	if v.RasterLine != badLineRasterStart {
		return
	}
	if v.registers[regControl1]&0x10 != 0 {
		v.denLatch = true
	}
	if int(v.Dot)/8+1 == maxCyclesPerLine {
		v.allowBadLine = v.denLatch
		v.denLatch = false
	}
}

// cycleIsBadLine evaluates the Bad Line Condition (section 3.5): raster
// within $30-$F7, its lower 3 bits matching YSCROLL, and DEN having been
// set at some point during raster line $30.
func (v *VICII) cycleIsBadLine() {
	yscroll := v.registers[regControl1] & 0x07
	v.badLine = v.RasterLine >= badLineRasterStart && v.RasterLine <= badLineRasterEnd &&
		uint8(v.RasterLine)&0x07 == yscroll && v.allowBadLine

	// "The transition from idle to display state occurs as soon as there
	// is a Bad Line Condition" (section 3.7.1).
	if v.badLine {
		v.idle = false
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

// cycleIsCAccess pulls BA low for the duration of a Bad Line's c-accesses,
// cycles 12-54 (section 3.7.2).
func (v *VICII) cycleIsCAccess() {
	if v.badLine {
		v.BA = false
	}
}

// cycleGAccess reads one row of character data (standard text mode only
// for now) into the graphics data sequencer, and advances VC/VMLI
// (section 3.7.2/3.7.3.1).
func (v *VICII) cycleGAccess() {
	v.videoBuffer = v.videoMatrixColor[v.VMLI]

	cb := (uint16(v.registers[regMemPointers]) >> 1) & 0x07
	addr := (cb << 11) + (v.videoBuffer&0xFF)<<3 + uint16(v.RC)
	v.gdSequencer = pla.VICLoad(addr)

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
	if v.RasterLine == bottomComp[rsel] {
		v.verticalBorder = true
	}
	if v.RasterLine == topComp[rsel] && v.registers[regControl1]&0x10 != 0 {
		v.verticalBorder = false
	}
}

// phi0high runs on the 4th dot of every 8-dot cycle: it performs a Bad
// Line's c-access (cycles 15-54), ticks the CIAs, and hands the bus to the
// CPU for Phi2.
func (v *VICII) phi0high() {
	cycle := int(v.Dot)/8 + 1
	if cycle >= 15 && cycle <= 54 {
		v.cycleCAccess()
	}

	cia1.Tick()
	cia2.Tick()

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
	char := pla.VICLoad((vm << 10) + v.VC)
	color := ram[0xD800+v.VC] & 0x0F
	v.videoMatrixColor[v.VMLI] = uint16(color)<<8 | uint16(char)
}

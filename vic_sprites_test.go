package tiny64

import (
	"testing"
)

// paintDots paints the dots [from, to) into the frame buffer without
// moving the beam, which is what the tests below want to look at.
//
// It goes through dotclock4, the only way to paint: four dots decided and
// written together. So the range has to divide into those groups, and a
// test that changes something part way through a run has to do it on a
// group boundary - which is also true of the machine, since the CPU only
// gets the bus between half-phases.
func paintDots(t *testing.T, v *VICII, from, to uint16) {
	t.Helper()
	if from%(DotsPerCycle/2) != 0 || to%(DotsPerCycle/2) != 0 {
		t.Fatalf("dots %d to %d are not whole half-phases", from, to)
	}
	for dot := from; dot < to; dot += DotsPerCycle / 2 {
		v.dotclock4(dot, noReloadDot, false)
	}
}

// TestVICSpriteSingleColorRendering verifies that a single-color sprite
// draws at the configured X/Y position with the individual sprite color.
func TestVICSpriteSingleColorRendering(t *testing.T) {
	parkMachine(t)
	ClearFrameBuffer()

	v := &VICII{}
	v.Reset()

	// Enable VIC bank 0
	cia.setVICBank(0)

	// Setup screen at $0400 (memPointers = 0x14)
	v.memPointers = 0x14
	// Sprite 0 pointer at $03F8 set to 64 -> sprite data at $1000
	ram[0x0400+0x03F8] = 64

	// Write 3 bytes per row for sprite 0 at $1000:
	// Row 0: 0xFF, 0x00, 0xAA -> 8 pixels on, 8 off, 4 on/4 off
	ram[0x1000] = 0xFF
	ram[0x1001] = 0x00
	ram[0x1002] = 0xAA

	// Set Sprite 0 position: X=24 (dot 48), Y=50 (raster line 50)
	v.WriteRegister(0xD000, 24) // X low
	v.WriteRegister(0xD001, 55) // Y=55 (line 55, inside graphics display area)
	v.WriteRegister(0xD010, 0)  // X MSB = 0

	// Set Sprite 0 color to Red (2)
	v.WriteRegister(0xD027, 2)

	// Enable Sprite 0
	v.WriteRegister(0xD015, 0x01)

	// DEN=1, RSEL=1, CSEL=1 (40 cols)
	v.control1 = 0x1B
	v.control2 = 0x08

	// Advance VIC to line 55, dot 48 (start of sprite 0)
	for v.rasterLine != 56 || v.slot != 6 {
		v.StepCycle()
	}

	// One bus cycle paints dots 48 to 55 - pixels 0-7 of byte 0, $FF, so
	// all eight are red. Stepping rather than painting the dots by hand
	// matters because dot 48 is also where the left comparison
	// opens the border, and that happens inside the cycle that paints it.
	v.StepCycle()

	// Verify byte 0 drawn pixels (dots 48..55 on line 56) match Red palette color
	buf := FrameBufferRGBA()
	redColor := C64Palette[2]
	for dot := uint16(48); dot < 56; dot++ {
		idx := (int(56-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}
		if got != redColor {
			t.Errorf("dot %d on line 56 = %v, want %v (Red)", dot, got, redColor)
		}
	}
}

// TestVICSpriteMulticolorRendering verifies multicolor sprite bit pairs:
// 01 -> Extra Color 0 ($D025), 10 -> Sprite Color ($D027), 11 -> Extra Color 1 ($D026).
func TestVICSpriteMulticolorRendering(t *testing.T) {
	parkMachine(t)
	ClearFrameBuffer()

	v := &VICII{}
	v.Reset()

	cia.setVICBank(0)
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64

	// Row 0 pattern: 0b01101100 = 0x6C in byte 0 -> pair 01 (extra 0), pair 10 (sprite color), pair 11 (extra 1), pair 00 (transparent)
	ram[0x1000] = 0x6C
	ram[0x1001] = 0x00
	ram[0x1002] = 0x00

	v.WriteRegister(0xD000, 24) // X=24 (dot 48)
	v.WriteRegister(0xD001, 55) // Y=55
	v.WriteRegister(0xD010, 0)
	v.WriteRegister(0xD015, 0x01) // Enable Sprite 0
	v.WriteRegister(0xD01C, 0x01) // Multicolor Sprite 0

	v.WriteRegister(0xD025, 3) // Extra Color 0 = Cyan (3)
	v.WriteRegister(0xD027, 4) // Sprite 0 Color = Purple (4)
	v.WriteRegister(0xD026, 5) // Extra Color 1 = Green (5)

	v.control1 = 0x1B
	v.control2 = 0x08
	for v.rasterLine != 56 || v.slot != 6 {
		v.StepCycle()
	}

	// One bus cycle paints dots 48 to 55: four pairs of two dots each.
	v.StepCycle()

	buf := FrameBufferRGBA()
	cyan := C64Palette[3]
	purple := C64Palette[4]
	green := C64Palette[5]

	// Pair 01 (dots 48, 49) -> Cyan
	for dot := uint16(48); dot <= 49; dot++ {
		idx := (int(56-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != cyan {
			t.Errorf("pair 01 at dot %d = %v, want Cyan %v", dot, got, cyan)
		}
	}
	// Pair 10 (dots 50, 51) -> Purple
	for dot := uint16(50); dot <= 51; dot++ {
		idx := (int(56-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != purple {
			t.Errorf("pair 10 at dot %d = %v, want Purple %v", dot, got, purple)
		}
	}
	// Pair 11 (dots 52, 53) -> Green
	for dot := uint16(52); dot <= 53; dot++ {
		idx := (int(56-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != green {
			t.Errorf("pair 11 at dot %d = %v, want Green %v", dot, got, green)
		}
	}
}

// TestVICSpriteExpansionXY tests 2x horizontal and vertical expansion.
func TestVICSpriteExpansionXY(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()
	cia.setVICBank(0)
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64
	ram[0x1000] = 0x80 // row 0, 1 bit set at px=0
	ram[0x1003] = 0x80 // row 1, 1 bit set at px=0
	ram[0x1006] = 0x00 // row 2, blank

	v.WriteRegister(0xD000, 24) // X=24
	v.WriteRegister(0xD001, 55) // Y=55
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD017, 0x01) // Expand Y
	v.WriteRegister(0xD01D, 0x01) // Expand X
	v.WriteRegister(0xD027, 2)    // Red

	v.control1 = 0x1B
	v.control2 = 0x08

	// Sprite Y names the line the DMA is triggered on, so the first
	// displayed line is 56. The expansion flip flop is set by the trigger
	// and inverted once per line thereafter, which makes row 0 occupy a
	// single line and every later row occupy two.
	for line := uint16(56); line <= 59; line++ {
		for cycles := 0; v.rasterLine != line || v.slot != 6; cycles++ {
			if cycles >= CyclesPerFrame {
				t.Fatalf("did not reach line %d, dot 48 within a frame", line)
			}
			if v.Dot()%DotsPerCycle != 0 {
				t.Fatalf("StepCycle starting at unaligned dot %d on raster line %d", v.Dot(), v.rasterLine)
			}
			v.StepCycle()
		}
		v.StepCycle()
	}

	buf := FrameBufferRGBA()
	red := C64Palette[2]
	at := func(line, dot uint16) [4]byte {
		idx := (int(line-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		return [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}
	}

	// Row 0 on line 56, row 1 held across lines 57 and 58, row 2 blank on 59.
	for _, line := range []uint16{56, 57, 58} {
		// Expand X makes bit 0 span two dots.
		for dot := uint16(48); dot <= 49; dot++ {
			if got := at(line, dot); got != red {
				t.Errorf("expanded pixel at line %d, dot %d = %v, want Red %v", line, dot, got, red)
			}
		}
		// Bit 1 is clear, so dots 50 and 51 must not be sprite coloured.
		for dot := uint16(50); dot <= 51; dot++ {
			if got := at(line, dot); got == red {
				t.Errorf("line %d, dot %d = Red, want background", line, dot)
			}
		}
	}
	if got := at(59, 48); got == red {
		t.Errorf("line 59, dot 48 = Red, want background (row 2 is blank)")
	}
}

// TestVICSpritePriority verifies $D01B priority (front vs behind foreground graphics).
func TestVICSpritePriority(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()
	cia.setVICBank(0)
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64
	ram[0x1000] = 0xFF

	v.WriteRegister(0xD000, 24)
	v.WriteRegister(0xD001, 55)
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD027, 2) // Red

	v.control1 = 0x1B
	v.control2 = 0x08
	for v.rasterLine != 56 || v.slot != 6 {
		v.StepCycle()
	}

	// Dot 48 is the first dot inside the display window, and the beam is
	// parked just before it, so the border flip-flop is still closed - the
	// left comparison runs at the head of the cycle that paints dot 48.
	// The tests below place the beam on that dot by hand rather than
	// stepping the cycle, because stepping it would run the g-access and
	// overwrite the sequencer contents each one sets up. Opening the
	// border here is the rest of that same hand placement.
	v.mainBorder = false

	// The colours are compared as the compositor decides them rather than
	// through the frame buffer: graphicsPixel is what this test is about,
	// and what the dot path then does with its answer - four dots to a
	// write - is dotclock4's business.
	const red, white = 2, 1

	// Test 1: Priority = 0 (sprite in front of graphics). Sprite (Red) shows over foreground graphics (White).
	v.WriteRegister(0xD01B, 0x00)
	v.gdSequencer = 0x4000 // foreground graphics pixel, two bits a dot
	v.videoBuffer = 0x0100 // color 1 (White)
	// The colours the sequencer emits are derived from videoBuffer when it
	// is latched, so setting it by hand has to derive them again. Without
	// this the test reads whatever palette the previous test left behind,
	// and passes or fails on the order the suite happens to run in.
	v.refreshGraphicsPalette()
	v.slot = 6
	if got := v.graphicsPixel(v.Dot()) & 0x0F; got != red {
		t.Errorf("priority=0 sprite pixel over foreground = %d, want Red %d", got, red)
	}

	// Test 2: Priority = 1 (sprite behind graphics). Foreground graphics (White) shows over sprite.
	v.WriteRegister(0xD01B, 0x01)
	v.gdSequencer = 0x4000 // foreground graphics pixel, two bits a dot
	v.videoBuffer = 0x0100 // color 1 (White)
	// The colours the sequencer emits are derived from videoBuffer when it
	// is latched, so setting it by hand has to derive them again. Without
	// this the test reads whatever palette the previous test left behind,
	// and passes or fails on the order the suite happens to run in.
	v.refreshGraphicsPalette()
	v.slot = 6
	if got := v.graphicsPixel(v.Dot()) & 0x0F; got != white {
		t.Errorf("priority=1 sprite pixel under foreground = %d, want White %d", got, white)
	}

	// Test 3: Priority = 1 (sprite behind graphics). Over background graphics (gdSequencer=0), sprite (Red) shows.
	v.WriteRegister(0xD01B, 0x01)
	v.gdSequencer = 0x00 // background graphics pixel
	v.slot = 6
	if got := v.graphicsPixel(v.Dot()) & 0x0F; got != red {
		t.Errorf("priority=1 sprite pixel over background = %d, want Red %d", got, red)
	}
}

// TestVICSpriteSpriteCollision verifies $D01E and sprite-sprite collision IRQ.
func TestVICSpriteSpriteCollision(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()
	cia.setVICBank(0)
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64 // Sprite 0 pointer
	ram[0x0400+0x03F9] = 64 // Sprite 1 pointer
	ram[0x1000] = 0x80      // Non-transparent pixel at px=0

	// Position both Sprite 0 and Sprite 1 at X=24, Y=55
	v.WriteRegister(0xD000, 24)
	v.WriteRegister(0xD001, 55)
	v.WriteRegister(0xD002, 24)
	v.WriteRegister(0xD003, 55)

	v.WriteRegister(0xD015, 0x03) // Enable Sprites 0 and 1
	v.WriteRegister(0xD01A, 0x04) // Enable Sprite-Sprite Collision IRQ (bit 2)

	v.control1 = 0x1B
	v.control2 = 0x08
	for v.rasterLine != 56 || v.slot != 6 {
		v.StepCycle()
	}
	v.slot = 6
	v.graphicsPixel(v.Dot())

	// Check IRQ fired
	if !v.IRQ {
		t.Errorf("v.IRQ = false after sprite-sprite collision, want true")
	}

	// Read $D01E: should return 0x03 (sprites 0 and 1 collided) and clear to 0
	if got := v.ReadRegister(0xD01E); got != 0x03 {
		t.Errorf("ReadRegister($D01E) = 0x%02X, want 0x03", got)
	}
	if got := v.ReadRegister(0xD01E); got != 0x00 {
		t.Errorf("ReadRegister($D01E) second read = 0x%02X, want 0x00 (read-cleared)", got)
	}
}

// TestVICSpriteDataCollision verifies $D01F and sprite-data collision IRQ.
func TestVICSpriteDataCollision(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()
	cia.setVICBank(0)
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64
	ram[0x1000] = 0x80

	v.WriteRegister(0xD000, 24)
	v.WriteRegister(0xD001, 55)
	v.WriteRegister(0xD015, 0x01) // Enable Sprite 0
	v.WriteRegister(0xD01A, 0x08) // Enable Sprite-Data Collision IRQ (bit 3)

	v.control1 = 0x1B
	v.control2 = 0x08
	for v.rasterLine != 56 || v.slot != 6 {
		v.StepCycle()
	}

	// A sprite-data collision needs a foreground graphics pixel under the
	// sprite pixel, and which dots the g-access makes foreground is
	// whatever the character ROM the VIC sees at $1000 happens to hold -
	// $80 in RAM there is the sprite's shape, not the character. So the
	// sequencer is loaded by hand, as TestVICSpritePriority does, and the
	// beam placed on the dot the sprite starts at.
	v.gdSequencer = 0x4000 // foreground graphics pixel, two bits a dot
	v.videoBuffer = 0x0100
	v.refreshGraphicsPalette()
	v.slot = 6
	v.graphicsPixel(v.Dot())

	if !v.IRQ {
		t.Errorf("v.IRQ = false after sprite-data collision, want true")
	}

	if got := v.ReadRegister(0xD01F); got != 0x01 {
		t.Errorf("ReadRegister($D01F) = 0x%02X, want 0x01", got)
	}
	if got := v.ReadRegister(0xD01F); got != 0x00 {
		t.Errorf("ReadRegister($D01F) second read = 0x%02X, want 0x00 (read-cleared)", got)
	}
}

// TestVICSpriteXMSBForSprites1To7 verifies that setting bit i in $D010 places
// sprite i (1..7) at X = x + 256 rather than scaling by 2^i.
func TestVICSpriteXMSBForSprites1To7(t *testing.T) {
	parkMachine(t)
	ClearFrameBuffer()

	v := &VICII{}
	v.Reset()
	cia.setVICBank(0)
	v.memPointers = 0x14

	// Sprite 1 pointer at $03F9 set to 64 -> sprite data at $1000
	ram[0x0400+0x03F9] = 64
	ram[0x1000] = 0xFF // 8 pixels on in byte 0

	// Set Sprite 1 position: X = 0, $D010 bit 1 set -> total X = 256 (startDot = 24 + 256 = 280)
	v.WriteRegister(0xD002, 0)    // Sprite 1 X low = 0
	v.WriteRegister(0xD003, 55)   // Sprite 1 Y = 55
	v.WriteRegister(0xD010, 0x02) // Sprite 1 X MSB = 1
	v.WriteRegister(0xD028, 2)    // Sprite 1 color = Red (2)
	v.WriteRegister(0xD015, 0x02) // Enable Sprite 1

	v.control1 = 0x1B
	v.control2 = 0x08

	for v.rasterLine != 56 || v.slot != 35 {
		v.StepCycle()
	}

	// The beam does not move: this paints the cycle's eight dots by
	// naming them, which is what the dot path itself does now.
	paintDots(t, v, v.Dot(), v.Dot()+DotsPerCycle)

	buf := FrameBufferRGBA()
	redColor := C64Palette[2]
	for dot := uint16(280); dot < 288; dot++ {
		idx := (int(56-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}
		if got != redColor {
			t.Errorf("sprite 1 at dot %d on line 56 = %v, want %v (Red)", dot, got, redColor)
		}
	}
}

func TestVICWrappedSpritesFillLeftBorderBlock(t *testing.T) {
	ClearFrameBuffer()

	// Line 56 is picked by hand, so the cached line state has to be derived
	// from it rather than asserted alongside it - see the comment in
	// TestVICSideBorderWriteAtRightCompareDot.
	v := &VICII{
		slot:          0,
		rasterLine:    56,
		spriteDisplay: 3,
		// Sprite 0: X=480, which wraps to dots 0-23. Sprite 1: X=0,
		// which starts at dot 24.
		spriteX:     [8]uint16{0x1E0, 0x000},
		spriteColor: [8]uint8{2, 4},
		spriteShape: [8][3]uint8{
			{0xFF, 0xFF, 0xFF},
			{0xFF, 0xFF, 0xFF},
		},
	}
	v.syncLineVisibility()
	// Which dots a sprite covers is cached the same way the line state
	// above is, and for the same reason: the registers were set by hand
	// rather than reached through WriteRegister or a line's latch. What
	// each of those dots paints is cached too, and latchSpriteShape is
	// what normally works it out from the bytes assigned above.
	v.rebuildSpriteCoverage()
	v.decodeSpriteRows()

	// Four cycles' worth of dots, named rather than stepped.
	paintDots(t, v, v.Dot(), v.Dot()+4*DotsPerCycle)

	for dot := uint16(0); dot < 24; dot++ {
		if !frameBufferPixelIs(dot, 56, 2) {
			t.Errorf("wrapped sprite pixel at dot %d is %v, want %v",
				dot, frameBufferPixelRGBA(dot, 56), C64Palette[2])
		}
	}
	for dot := uint16(24); dot < 32; dot++ {
		if !frameBufferPixelIs(dot, 56, 4) {
			t.Errorf("adjacent sprite pixel at dot %d is %v, want %v",
				dot, frameBufferPixelRGBA(dot, 56), C64Palette[4])
		}
	}
}

// displayedLines runs the per line sprite DMA latch over a range of raster
// lines and reports the lines on which sprite 0 is displayed. The latch runs
// at slot 44, after that line's display window, so state latched on line L
// drives the display of line L+1.
func displayedLines(v *VICII, from, to uint16, at map[uint16]func()) []uint16 {
	var lines []uint16
	for line := from; line <= to; line++ {
		v.rasterLine = line
		if f, ok := at[line]; ok {
			f()
		}
		v.latchSpriteDisplay()
		if v.spriteDisplay&1 != 0 {
			lines = append(lines, line+1)
		}
	}
	return lines
}

// TestVICSpriteDMABandIsYPlusOne verifies that the sprite Y register names the
// line the DMA is triggered on rather than the first line drawn. The chip
// turns the display on at cycle 58, past that line's display window, so a
// sprite at Y occupies lines Y+1 to Y+21.
func TestVICSpriteDMABandIsYPlusOne(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD001, 55)

	got := displayedLines(v, 40, 90, nil)
	if len(got) != 21 {
		t.Fatalf("displayed %d lines %v, want 21", len(got), got)
	}
	if got[0] != 56 || got[20] != 76 {
		t.Errorf("displayed lines %d..%d, want 56..76", got[0], got[20])
	}
}

// TestVICSpriteDMATriggerIsOneShot verifies that DMA is a one shot trigger:
// once a sprite is under DMA the chip walks 21 rows off its own counter and
// never consults Y again. Raster multiplexers depend on this, because they
// rewrite Y mid band to reuse the same sprite further down the screen.
func TestVICSpriteDMATriggerIsOneShot(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD001, 55)

	// Move the sprite mid band to a line the raster has not yet reached but
	// which still falls inside the band. A chip that re-armed on every Y
	// match would restart the run at line 65 and stretch the band; the real
	// one ignores Y entirely until the 21 rows are spent. The later write
	// then starts a fresh band once DMA has switched off.
	got := displayedLines(v, 40, 200, map[uint16]func(){
		60:  func() { v.WriteRegister(0xD001, 65) },
		100: func() { v.WriteRegister(0xD001, 150) },
	})

	want := make([]uint16, 0, 42)
	for line := uint16(56); line <= 76; line++ {
		want = append(want, line)
	}
	for line := uint16(151); line <= 171; line++ {
		want = append(want, line)
	}
	if len(got) != len(want) {
		t.Fatalf("displayed %d lines, want %d: got %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("displayed lines %v, want %v", got, want)
		}
	}
}

// TestVICSpriteShapeIsLatchedPerLine verifies that the three pattern bytes a
// sprite draws are fetched once per line, in that sprite's own slot of the
// fetch block, and not re-read from memory as each pixel is painted.
//
// This is what makes a raster multiplexer work. The routine that rewrites a
// sprite's pointer part way down the screen runs while the line it belongs to
// is still being drawn, so a VIC that re-read the pointer per pixel would
// switch shape mid-line and truncate the sprite.
func TestVICSpriteShapeIsLatchedPerLine(t *testing.T) {
	parkMachine(t)
	ClearFrameBuffer()

	v := &VICII{}
	v.Reset()
	cia.setVICBank(0)
	v.memPointers = 0x14

	ram[0x0400+0x03F8] = 64 // sprite 0 data at $1000
	ram[0x1000] = 0xFF      // row 0, all eight pixels set

	v.WriteRegister(0xD000, 24) // X=24 -> dot 48
	v.WriteRegister(0xD001, 55)
	v.WriteRegister(0xD010, 0)
	v.WriteRegister(0xD027, 2) // Red
	v.WriteRegister(0xD015, 0x01)
	v.control1 = 0x1B
	v.control2 = 0x08

	for v.rasterLine != 56 || v.slot != 6 {
		v.StepCycle()
	}

	// Pull the shape out from under the sprite mid-line, exactly as a
	// multiplexer would. Line 56 was already fetched during line 55, so
	// every pixel of it must still be drawn.
	for addr := uint16(0x1000); addr < 0x1040; addr++ {
		ram[addr] = 0
	}
	ram[0x0400+0x03F8] = 65

	v.StepCycle()

	red := C64Palette[2]
	for dot := uint16(48); dot < 56; dot++ {
		if got := frameBufferPixelRGBA(dot, 56); got != red {
			t.Errorf("dot %d on line 56 = %v, want Red %v (shape was latched on line 55)", dot, got, red)
		}
	}

	// The latch is per line, not a one-off: line 57 fetches again during
	// line 56 and so must pick up the new, blank shape.
	for v.rasterLine != 57 || v.slot != 6 {
		v.StepCycle()
	}
	v.StepCycle()
	for dot := uint16(48); dot < 56; dot++ {
		if got := frameBufferPixelRGBA(dot, 57); got == red {
			t.Errorf("dot %d on line 57 = Red, want background (shape was re-fetched on line 56)", dot)
		}
	}
}

// TestVICSpriteYCompareWrapsAtEightBits verifies that the DMA trigger compares
// the sprite Y against the low eight bits of RASTER. On PAL the raster runs to
// 311, so a sprite parked above line 56 is triggered a second time as the
// raster passes 256+Y, and its band wraps the frame boundary.
func TestVICSpriteYCompareWrapsAtEightBits(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD001, 50)

	got := displayedLines(v, 40, 311, nil)
	if len(got) < 22 {
		t.Fatalf("displayed %d lines %v, want both bands", len(got), got)
	}
	if got[0] != 51 || got[20] != 71 {
		t.Errorf("first band %d..%d, want 51..71", got[0], got[20])
	}
	// 256+50 = 306, so the second band opens on 307.
	if got[21] != 307 {
		t.Errorf("second band starts at %d, want 307 (raster 306 matches Y=50 in eight bits)", got[21])
	}
}

// TestVICSpriteDisableDoesNotRetractBand verifies that MxE is consulted only
// when arming the DMA. Once a sprite is under DMA the run is committed and
// clearing its enable bit cannot cut it short; the sprite stops when its rows
// are spent. A multiplexer that disables a sprite after handing it off relies
// on the rows already in flight still being drawn.
func TestVICSpriteDisableDoesNotRetractBand(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD001, 55)

	got := displayedLines(v, 40, 120, map[uint16]func(){
		60: func() { v.WriteRegister(0xD015, 0x00) },
	})
	if len(got) != 21 {
		t.Fatalf("displayed %d lines %v, want the full 21 line band", len(got), got)
	}
	if got[0] != 56 || got[20] != 76 {
		t.Errorf("displayed lines %d..%d, want 56..76", got[0], got[20])
	}
}

// TestSpriteDMAPullsBALow verifies that a sprite under DMA actually drives BA
// low over its window, rather than the window merely being described by
// spriteBASlotMask. Sprite 0 fetches in slots 47 and 48 and BA leads the grab
// by three cycles, so BA is low across slots 44 to 48 and high either side.
func TestSpriteDMAPullsBALow(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()
	cia.setVICBank(0)
	v.memPointers = 0x14
	v.WriteRegister(0xD001, 55)
	v.WriteRegister(0xD015, 0x01)
	v.control1 = 0x1B
	v.control2 = 0x08

	// Line 56 is inside the band, so sprite 0 is under DMA for the whole
	// of that line's fetch block. Bad Line BA is confined to slots 1-43,
	// which leaves slots 44 and up to the sprites alone.
	for v.rasterLine != 56 || v.slot != 0 {
		v.StepCycle()
	}
	for slot := uint16(43); slot <= 50; slot++ {
		// BA is driven by the slot's phi0low, four dots into the cycle,
		// and nothing touches it again until the next cycle's phi0low -
		// phi0high only copies it into AEC. Sampling at the end of the
		// slot's cycle therefore reads the value phi0low just set.
		for v.Dot() != (slot+1)*DotsPerCycle {
			v.StepCycle()
		}
		want := slot < 44 || slot > 48 // BA high outside sprite 0's window
		if v.BA != want {
			t.Errorf("slot %d: BA = %v, want %v", slot, v.BA, want)
		}
	}
}

// TestSpriteCoverageStaysConsistentAcrossAFrame is the guard on the
// coverage table's invalidation rule. The table is derived ahead of the
// dots that read it and rebuilt only where its inputs change, so a new
// writer of a sprite X, of $D010, of $D01D, or of spriteDisplay that does
// not rebuild would leave the compositor painting sprites in the wrong
// place - or, worse, painting none at all, which no assertion about
// colours would notice on a fixture that expects background there.
//
// So drive a frame with a program moving sprites under the beam, and after
// every bus cycle check the cached table still equals one built from the
// registers as they stand. Anything that changes an input without
// rebuilding fails on the cycle it happens.
func TestSpriteCoverageStaysConsistentAcrossAFrame(t *testing.T) {
	saveMachine(t)
	Reset()
	loadSpriteProgram()

	// Walk every sprite's X, the ninth bits and the expansion register, so
	// the sweep below sees windows move under it rather than sit still.
	copy(Ram()[0x0800:], []byte{
		0xE6, 0x20, // INC $20
		0xA5, 0x20, // LDA $20
		0x8D, 0x00, 0xD0, // STA $D000
		0x8D, 0x02, 0xD0, // STA $D002
		0x8D, 0x0E, 0xD0, // STA $D00E
		0x8D, 0x10, 0xD0, // STA $D010
		0x8D, 0x1D, 0xD0, // STA $D01D
		0x4C, 0x00, 0x08, // JMP $0800
	})
	cpu.PC = 0x0800

	for cycle := range CyclesPerFrame {
		vic.StepCycle()

		cachedCoverage, cachedStart := vic.spriteCoverage, vic.spriteStart
		vic.rebuildSpriteCoverage()
		if vic.spriteCoverage != cachedCoverage {
			// Report the first dot that differs rather than two 512 byte
			// arrays.
			for dot := range vic.spriteCoverage {
				if got, want := cachedCoverage[dot], vic.spriteCoverage[dot]; got != want {
					t.Fatalf("cycle %d (raster %d dot %d): coverage of dot %d held %#02x, rebuilding from the same registers gives %#02x - something moved a sprite without rebuilding",
						cycle, vic.rasterLine, vic.Dot(), dot, got, want)
				}
			}
		}
		if vic.spriteStart != cachedStart {
			t.Fatalf("cycle %d (raster %d): sprite start dots held %v, rebuilding gives %v",
				cycle, vic.rasterLine, cachedStart, vic.spriteStart)
		}
	}
}

// TestVICSpriteMulticolorWriteMidLineRedecodesRow covers the case the
// decoded row can get wrong: $D01C changes what a shape byte means -
// eight hires pixels or four multicolour pairs - without changing the
// bytes themselves, so a row worked out before the write describes the
// wrong mode after it.
//
// The shape is all ones, which is every dot in the sprite's own colour
// read as hires, and four pairs of 11 - $D026 - read as multicolour. So
// the dots either side of the write are two different colours, and a row
// that failed to be worked out again would paint them the same.
func TestVICSpriteMulticolorWriteMidLineRedecodesRow(t *testing.T) {
	ClearFrameBuffer()

	// Hand-built for the same reason the wrapped sprite test above is,
	// and cached state derived rather than asserted for the same reason.
	v := &VICII{
		slot:          0,
		rasterLine:    56,
		spriteDisplay: 1,
		spriteX:       [8]uint16{0}, // dots 24 to 47
		spriteColor:   [8]uint8{2},
		spriteMC1:     5,
		spriteShape:   [8][3]uint8{{0xFF, 0xFF, 0xFF}},
	}
	v.syncLineVisibility()
	v.rebuildSpriteCoverage()
	v.decodeSpriteRows()

	paintDots(t, v, 24, 32)
	v.WriteRegister(0xD01C, 0x01)
	paintDots(t, v, 32, 48)

	for dot := uint16(24); dot < 32; dot++ {
		if !frameBufferPixelIs(dot, 56, 2) {
			t.Errorf("dot %d painted before the $D01C write is %v, want %v (hires, the sprite's own colour)",
				dot, frameBufferPixelRGBA(dot, 56), C64Palette[2])
		}
	}
	for dot := uint16(32); dot < 48; dot++ {
		if !frameBufferPixelIs(dot, 56, 5) {
			t.Errorf("dot %d painted after the $D01C write is %v, want %v (multicolour pair 11, $D026)",
				dot, frameBufferPixelRGBA(dot, 56), C64Palette[5])
		}
	}
}

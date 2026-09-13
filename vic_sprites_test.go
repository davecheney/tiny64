package tiny64

import (
	"testing"
)

// TestVICSpriteSingleColorRendering verifies that a single-color sprite
// draws at the configured X/Y position with the individual sprite color.
func TestVICSpriteSingleColorRendering(t *testing.T) {
	clearFrameBufferRGBA()

	v := &VICII{}
	v.Reset()

	// Enable VIC bank 0
	cia2.PRA, cia2.DDRA = 3, 3

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
	for v.rasterLine != 55 || v.dot != 48 {
		v.StepDot()
	}

	// Step 8 dots (pixels 0-7, byte 0 = 0xFF -> all red = color 2)
	for range 8 {
		v.paintGraphicsPixel()
		v.dot++
	}

	// Verify byte 0 drawn pixels (dots 48..55 on line 55) match Red palette color
	buf := FrameBufferRGBA()
	redColor := C64Palette[2]
	for dot := uint16(48); dot < 56; dot++ {
		idx := (int(55-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}
		if got != redColor {
			t.Errorf("dot %d on line 50 = %v, want %v (Red)", dot, got, redColor)
		}
	}
}

// TestVICSpriteMulticolorRendering verifies multicolor sprite bit pairs:
// 01 -> Extra Color 0 ($D025), 10 -> Sprite Color ($D027), 11 -> Extra Color 1 ($D026).
func TestVICSpriteMulticolorRendering(t *testing.T) {
	clearFrameBufferRGBA()

	v := &VICII{}
	v.Reset()

	cia2.PRA, cia2.DDRA = 3, 3
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
	for v.rasterLine != 55 || v.dot != 48 {
		v.StepDot()
	}

	// Paint 8 dots (4 pairs of 2 dots each)
	for range 8 {
		v.paintGraphicsPixel()
		v.dot++
	}

	buf := FrameBufferRGBA()
	cyan := C64Palette[3]
	purple := C64Palette[4]
	green := C64Palette[5]

	// Pair 01 (dots 48, 49) -> Cyan
	for dot := uint16(48); dot <= 49; dot++ {
		idx := (int(55-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != cyan {
			t.Errorf("pair 01 at dot %d = %v, want Cyan %v", dot, got, cyan)
		}
	}
	// Pair 10 (dots 50, 51) -> Purple
	for dot := uint16(50); dot <= 51; dot++ {
		idx := (int(55-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != purple {
			t.Errorf("pair 10 at dot %d = %v, want Purple %v", dot, got, purple)
		}
	}
	// Pair 11 (dots 52, 53) -> Green
	for dot := uint16(52); dot <= 53; dot++ {
		idx := (int(55-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != green {
			t.Errorf("pair 11 at dot %d = %v, want Green %v", dot, got, green)
		}
	}
}

// TestVICSpriteExpansionXY tests 2x horizontal and vertical expansion.
func TestVICSpriteExpansionXY(t *testing.T) {
	v := &VICII{}
	v.Reset()
	cia2.PRA, cia2.DDRA = 3, 3
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64
	ram[0x1000] = 0x80 // 1 bit set at px=0

	v.WriteRegister(0xD000, 24) // X=24
	v.WriteRegister(0xD001, 55) // Y=55
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD017, 0x01) // Expand Y (42 lines)
	v.WriteRegister(0xD01D, 0x01) // Expand X (48 dots)
	v.WriteRegister(0xD027, 2)    // Red

	v.control1 = 0x1B
	v.control2 = 0x08

	for v.rasterLine != 55 || v.dot != 48 {
		v.StepDot()
	}

	// With Expand X, bit 0 (px=0) spans dots 48 and 49.
	// With Expand Y, row 0 spans lines 55 and 56.
	for line := uint16(55); line <= 56; line++ {
		v.rasterLine = line
		for dot := uint16(48); dot <= 51; dot++ {
			v.dot = dot
			v.paintGraphicsPixel()
		}
	}

	buf := FrameBufferRGBA()
	red := C64Palette[2]

	for line := uint16(55); line <= 56; line++ {
		for dot := uint16(48); dot <= 49; dot++ {
			idx := (int(line-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
			if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != red {
				t.Errorf("expanded pixel at line %d, dot %d = %v, want Red %v", line, dot, got, red)
			}
		}
	}
}

// TestVICSpritePriority verifies $D01B priority (front vs behind foreground graphics).
func TestVICSpritePriority(t *testing.T) {
	v := &VICII{}
	v.Reset()
	cia2.PRA, cia2.DDRA = 3, 3
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64
	ram[0x1000] = 0xFF

	v.WriteRegister(0xD000, 24)
	v.WriteRegister(0xD001, 55)
	v.WriteRegister(0xD015, 0x01)
	v.WriteRegister(0xD027, 2) // Red

	v.control1 = 0x1B
	v.control2 = 0x08
	for v.rasterLine != 55 || v.dot != 48 {
		v.StepDot()
	}

	buf := FrameBufferRGBA()
	red := C64Palette[2]
	white := C64Palette[1]
	idx := (int(55-FirstVisibleLine)*VisibleDotsPerLine + 48) * 4

	// Test 1: Priority = 0 (sprite in front of graphics). Sprite (Red) shows over foreground graphics (White).
	v.WriteRegister(0xD01B, 0x00)
	v.gdSequencer = 0x80   // foreground graphics pixel
	v.videoBuffer = 0x0100 // color 1 (White)
	v.dot = 48
	v.paintGraphicsPixel()
	if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != red {
		t.Errorf("priority=0 sprite pixel over foreground = %v, want Red %v", got, red)
	}

	// Test 2: Priority = 1 (sprite behind graphics). Foreground graphics (White) shows over sprite.
	v.WriteRegister(0xD01B, 0x01)
	v.gdSequencer = 0x80   // foreground graphics pixel
	v.videoBuffer = 0x0100 // color 1 (White)
	v.dot = 48
	v.paintGraphicsPixel()
	if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != white {
		t.Errorf("priority=1 sprite pixel under foreground = %v, want White %v", got, white)
	}

	// Test 3: Priority = 1 (sprite behind graphics). Over background graphics (gdSequencer=0), sprite (Red) shows.
	v.WriteRegister(0xD01B, 0x01)
	v.gdSequencer = 0x00 // background graphics pixel
	v.dot = 48
	v.paintGraphicsPixel()
	if got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}; got != red {
		t.Errorf("priority=1 sprite pixel over background = %v, want Red %v", got, red)
	}
}

// TestVICSpriteSpriteCollision verifies $D01E and sprite-sprite collision IRQ.
func TestVICSpriteSpriteCollision(t *testing.T) {
	v := &VICII{}
	v.Reset()
	cia2.PRA, cia2.DDRA = 3, 3
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
	for v.rasterLine != 55 || v.dot != 48 {
		v.StepDot()
	}
	v.dot = 48
	v.paintGraphicsPixel()

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
	v := &VICII{}
	v.Reset()
	cia2.PRA, cia2.DDRA = 3, 3
	v.memPointers = 0x14
	ram[0x0400+0x03F8] = 64
	ram[0x1000] = 0x80

	v.WriteRegister(0xD000, 24)
	v.WriteRegister(0xD001, 55)
	v.WriteRegister(0xD015, 0x01) // Enable Sprite 0
	v.WriteRegister(0xD01A, 0x08) // Enable Sprite-Data Collision IRQ (bit 3)

	// Set up text mode so nextGraphicsColor returns isForeground = true
	v.gdSequencer = 0x80 // top bit set
	v.videoBuffer = 0x0100

	v.control1 = 0x1B
	v.control2 = 0x08
	for v.rasterLine != 55 || v.dot != 48 {
		v.StepDot()
	}
	v.dot = 48
	v.paintGraphicsPixel()

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
	clearFrameBufferRGBA()

	v := &VICII{}
	v.Reset()
	cia2.PRA, cia2.DDRA = 3, 3
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

	for v.rasterLine != 55 || v.dot != 280 {
		v.StepDot()
	}

	for range 8 {
		v.paintGraphicsPixel()
		v.dot++
	}

	buf := FrameBufferRGBA()
	redColor := C64Palette[2]
	for dot := uint16(280); dot < 288; dot++ {
		idx := (int(55-FirstVisibleLine)*VisibleDotsPerLine + int(dot)) * 4
		got := [4]byte{buf[idx], buf[idx+1], buf[idx+2], buf[idx+3]}
		if got != redColor {
			t.Errorf("sprite 1 at dot %d on line 55 = %v, want %v (Red)", dot, got, redColor)
		}
	}
}

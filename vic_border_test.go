package tiny64

import "testing"

// TestVICBorderPlacement captures a full frame's pixels and reports the
// exact displayX column where the left border ends and the display
// window begins (and where the right border begins), so it can be
// compared against the expected geometry from section 3.9 of the VIC
// Article: for CSEL=1 (40 columns) the border/display transition happens
// at real X coordinate $18 (24) on the left and $158 (344) on the right.
func TestVICBorderPlacement(t *testing.T) {
	v := &VICII{}
	ClearFrameBuffer()
	v.Reset()

	v.WriteRegister(0xD020, 0x0E) // border: light blue (14)
	v.WriteRegister(0xD021, 0x06) // background: blue (6)
	v.WriteRegister(0xD011, 0x1B) // DEN=1, RSEL=1 (25 rows), YSCROLL=3
	v.WriteRegister(0xD016, 0x08) // CSEL=1 (40 cols), MCM=0, XSCROLL=0
	v.WriteRegister(0xD018, 0x14) // VM=1 (screen @ $0400), CB=2 (chars @ $1000)

	for col := range 40 {
		ram[0x0400+col] = byte(1 + col) // screen code 1 = 'A'
		colorRAM[col] = 0x01
	}

	for range CyclesPerFrame {
		v.StepCycle()
	}

	// Raster line 100 is safely inside the 25-row display window
	// (51-250) and outside the very first Bad Line's character row, so
	// c-accesses have long since populated the video matrix buffer.
	firstNonBorder := -1
	lastNonBorder := -1
	for x := range VisibleDotsPerLine {
		if !frameBufferPixelIs(uint16(x), 100, 0x0e) {
			if firstNonBorder == -1 {
				firstNonBorder = x
			}
			lastNonBorder = x
		}
	}
	t.Logf("row 100: first non-border pixel at displayX=%d, last at displayX=%d (row width=%d)", firstNonBorder, lastNonBorder, DotsPerLine)

	// The left comparison uses the visible dot directly. The VIC X counter
	// wraps before the right comparison, whose visible edge is dot 368.
	wantFirst := leftComp40
	wantLast := rightEdge40 - 1
	t.Logf("want first non-border displayX=%d", wantFirst)

	if firstNonBorder != wantFirst {
		t.Errorf("first non-border pixel at displayX=%d, want %d", firstNonBorder, wantFirst)
	}
	if lastNonBorder != wantLast {
		t.Errorf("last non-border pixel at displayX=%d, want %d", lastNonBorder, wantLast)
	}
	if !frameBufferPixelIs(uint16(rightEdge40), 100, 0x0e) {
		t.Errorf("pixel at first right-border displayX=%d is %v, want border color %v",
			rightEdge40, frameBufferPixelRGBA(uint16(rightEdge40), 100), C64Palette[0x0e])
	}
}

func TestVICBorderColorWriteSamplesCurrentPhi2Span(t *testing.T) {
	v := &VICII{}
	v.Reset()
	for dot := range v.rightBorder {
		v.rightBorder[dot] = 0x02
	}
	v.dot = rightEdge40 + DotsPerCycle

	v.WriteRegister(0xD020, 0x05)

	for dot := uint16(rightEdge38); dot < rightEdge40; dot++ {
		if got := v.rightBorder[dot-rightEdge38]; got != 0x02 {
			t.Fatalf("right-border color at dot %d = %d, want previous color 2", dot, got)
		}
	}
	for dot := uint16(rightEdge40); dot <= v.dot; dot++ {
		if got := v.rightBorder[dot-rightEdge38]; got != 0x05 {
			t.Fatalf("right-border color at dot %d = %d, want newly written color 5", dot, got)
		}
	}
	if got := v.rightBorder[v.dot+1-rightEdge38]; got != 0x02 {
		t.Fatalf("right-border color after current Phi2 span = %d, want previous color 2", got)
	}
}

// TestVICGAccessPixelAlignment is a regression test for a 4-pixel g-access
// pipeline delay bug: the graphics data sequencer was reloaded on the bus
// cycle boundary (dot&7==4) rather than on the character-cell boundary
// (dot&7==0) that the border comparators and pixel output work in. This
// made every column's leftmost ~4 pixels get shifted away before the
// border even opened (looking like the left border "occludes" the
// character), and left a ~4 pixel gap of stale/blank pixels before the
// right border resumed. This test uses a solid (0xFF) character bitmap
// for the first and last columns, so any misalignment shows up as
// background-colored pixels within what should be a fully solid
// 8-pixel-wide character cell.
func TestVICGAccessPixelAlignment(t *testing.T) {
	v := &VICII{}
	const targetRow = 52 // within the first Bad Line's row (raster $33-$3A)
	ClearFrameBuffer()
	v.Reset()

	v.WriteRegister(0xD020, 0x0E) // border: light blue (14)
	v.WriteRegister(0xD021, 0x06) // background: blue (6)
	v.WriteRegister(0xD011, 0x1B) // DEN=1, RSEL=1 (25 rows), YSCROLL=3
	v.WriteRegister(0xD016, 0x08) // CSEL=1 (40 cols), MCM=0, XSCROLL=0
	v.WriteRegister(0xD018, 0x10) // VM=1 (screen @ $0400), CB=0 (chars @ $0000, plain RAM)

	// Screen codes 0 and 1 (used for the first and last column) both get a
	// fully solid 8x8 bitmap, so their entire 8-pixel-wide cell should show
	// foreground color with no gaps if the pipeline delay is correct.
	for row := range 8 {
		ram[0x0000+row] = 0xFF // char 0's bitmap
		ram[0x0008+row] = 0xFF // char 1's bitmap
	}
	for col := range 40 {
		ram[0x0400+col] = 0
		colorRAM[col] = 0x01 // white foreground
	}
	ram[0x0400+39] = 1 // last column uses char 1 (also solid)

	for range CyclesPerFrame {
		v.StepCycle()
	}

	const foreground = 0x01
	wantFirst := uint16(leftComp40)     // displayX 48
	wantLast := uint16(rightEdge40 - 1) // displayX 367
	if !frameBufferPixelIs(wantFirst, targetRow, foreground) {
		t.Errorf("pixel at first column's leftmost displayX=%d is %v, want foreground %v (occluded by border)",
			wantFirst, frameBufferPixelRGBA(wantFirst, targetRow), C64Palette[foreground])
	}
	if !frameBufferPixelIs(wantLast, targetRow, foreground) {
		t.Errorf("pixel at last column's rightmost displayX=%d is %v, want foreground %v (gap before right border)",
			wantLast, frameBufferPixelRGBA(wantLast, targetRow), C64Palette[foreground])
	}
}

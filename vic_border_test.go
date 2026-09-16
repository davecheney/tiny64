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

// TestVICBorderColorWriteSamplesCurrentPhi2Span pins where a mid-line
// $D020 write takes effect in the right-border shadow buffer: from the
// first dot of the bus cycle the write was made in, leaving the dots
// painted before that cycle on the previous colour.
//
// The dots after the write are filled in here too. They have not been
// painted yet, but the colour they will be painted with is already known,
// and filling forwards is what lets the per-dot pixel path carry no
// shadow-buffer store at all; see sampleBorderColorAtWrite. The write is
// also what brings the buffer into force in the first place - before it,
// the span is described by v.borderColor alone.
func TestVICBorderColorWriteSamplesCurrentPhi2Span(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.borderColor = 0x02
	v.dot = rightEdge40 + DotsPerCycle

	v.WriteRegister(0xD020, 0x05)

	if !v.borderColorVaried {
		t.Fatal("borderColorVaried=false after a mid-line $D020 write, want the shadow buffer in force")
	}
	spanStart := v.dot - DotsPerCycle
	for dot := uint16(rightEdge38); dot < spanStart; dot++ {
		if got := v.rightBorder[dot-rightEdge38]; got != 0x02 {
			t.Fatalf("right-border color at dot %d = %d, want previous color 2", dot, got)
		}
	}
	for dot := spanStart; dot < VisibleDotsPerLine; dot++ {
		if got := v.rightBorder[dot-rightEdge38]; got != 0x05 {
			t.Fatalf("right-border color at dot %d = %d, want newly written color 5", dot, got)
		}
	}
}

// TestVICBorderColorWriteBeforeRightEdgeStaysUniform checks that a $D020
// write made before any dot of the replayed span has been painted leaves
// the uniform representation in force: every dot the replay covers will be
// painted with the new colour, so there is nothing per-dot to record.
func TestVICBorderColorWriteBeforeRightEdgeStaysUniform(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.borderColor = 0x02
	v.dot = rightEdge38 - DotsPerCycle

	v.WriteRegister(0xD020, 0x05)

	if v.borderColorVaried {
		t.Fatal("borderColorVaried=true after a $D020 write ahead of the right edge, want the uniform span")
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

// borderScheduledWrite is a register write performed at a chosen dot on a
// chosen raster line, from the same place in the cycle a CPU write lands:
// stepCycle ticks Phi2 last, so writing immediately after StepCycle returns
// puts the write at the same dot the CPU would have.
type borderScheduledWrite struct {
	line  uint16
	dot   uint16
	reg   uint16
	value uint8
}

// borderTestVIC sets up a VIC-II showing a full screen of characters in
// 40-column mode, the arrangement the right-border replay path runs on.
func borderTestVIC() *VICII {
	v := &VICII{}
	ClearFrameBuffer()
	v.Reset()
	v.WriteRegister(0xD020, 0x0E) // border: light blue (14)
	v.WriteRegister(0xD021, 0x06) // background: blue (6)
	v.WriteRegister(0xD011, 0x1B) // DEN=1, RSEL=1 (25 rows), YSCROLL=3
	v.WriteRegister(0xD016, 0x08) // CSEL=1 (40 cols)
	v.WriteRegister(0xD018, 0x14) // VM=1 (screen @ $0400), CB=2 (chars @ $1000)
	for col := range 40 {
		ram[0x0400+col] = byte(1 + col)
		colorRAM[col] = 0x01
	}
	return v
}

func runFrameWithWrites(v *VICII, writes []borderScheduledWrite) {
	for range CyclesPerFrame {
		v.StepCycle()
		for _, w := range writes {
			if v.rasterLine == w.line && v.dot == w.dot {
				v.WriteRegister(w.reg, w.value)
			}
		}
	}
}

// checkBorderRun asserts that every dot in [first, last] of the given
// raster line carries colorIndex.
func checkBorderRun(t *testing.T, line, first, last uint16, colorIndex byte) {
	t.Helper()
	for dot := first; dot <= last; dot++ {
		if !frameBufferPixelIs(dot, line, colorIndex) {
			t.Fatalf("line %d dot %d = %v, want color %d (%v)",
				line, dot, frameBufferPixelRGBA(dot, line), colorIndex, C64Palette[colorIndex&0x0f])
		}
	}
}

// TestVICBorderColorChangeMidLine pins the pixels the right-border replay
// produces when $D020 changes part way through the span it replays.
//
// Nothing from the right edge onwards is painted live with its final
// colour: the comparison that closes the main border flip-flop does not
// run until after the last visible dot, so paintGraphicsPixel puts
// graphics in the span and finishSideBorder paints over it at the line
// wrap. The replay is therefore the only thing that decides these pixels,
// and a $D020 write inside the span is the one case where they are not all
// the same colour - which is the whole reason the rightBorder shadow
// buffer exists. These cases assert screen contents, not how the buffer
// came to be filled, so they hold for any implementation of it.
func TestVICBorderColorChangeMidLine(t *testing.T) {
	t.Run("MidSpan", func(t *testing.T) {
		v := borderTestVIC()
		// Written on the bus cycle ending at dot 384, inside the replayed
		// span. The write is in force for the whole of that cycle, so it
		// reaches back to dot 376 and the dots before that keep the old
		// colour.
		runFrameWithWrites(v, []borderScheduledWrite{{line: 100, dot: 384, reg: 0xD020, value: 0x02}})

		checkBorderRun(t, 100, rightEdge40, 375, 0x0E)
		checkBorderRun(t, 100, 376, VisibleDotsPerLine-1, 0x02)
		// The colour stays changed, so the next line is uniform again,
		// and the line before it is untouched.
		checkBorderRun(t, 101, rightEdge40, VisibleDotsPerLine-1, 0x02)
		checkBorderRun(t, 99, rightEdge40, VisibleDotsPerLine-1, 0x0E)
	})

	t.Run("DuringHblank", func(t *testing.T) {
		v := borderTestVIC()
		// Dot 440 is past the last visible dot but before the line wrap:
		// too late for the line about to be replayed, so it applies from
		// the next one.
		runFrameWithWrites(v, []borderScheduledWrite{{line: 120, dot: 440, reg: 0xD020, value: 0x08}})

		checkBorderRun(t, 120, rightEdge40, VisibleDotsPerLine-1, 0x0E)
		checkBorderRun(t, 121, rightEdge40, VisibleDotsPerLine-1, 0x08)
	})

	t.Run("BeforeRightEdge40In38Columns", func(t *testing.T) {
		v := borderTestVIC()
		// CSEL=0 moves the replayed span's start to dot 359, so a write on
		// the cycle ending at dot 360 lands inside it. Before the
		// 40-column edge a write does not reach back over the dots its
		// cycle has already painted, so only dot 361 onwards changes.
		runFrameWithWrites(v, []borderScheduledWrite{
			{line: 139, dot: 440, reg: 0xD016, value: 0x00}, // CSEL=0 from line 140
			{line: 140, dot: 360, reg: 0xD020, value: 0x0A},
			{line: 140, dot: 440, reg: 0xD016, value: 0x08}, // back to CSEL=1
		})

		checkBorderRun(t, 140, rightEdge38, 360, 0x0E)
		checkBorderRun(t, 140, 361, VisibleDotsPerLine-1, 0x0A)
	})
}

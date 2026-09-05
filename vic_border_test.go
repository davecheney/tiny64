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
	const w, h = DotsPerLine, RasterLinesPerFrame
	const unwritten = 0xFF // sentinel: dot never painted (horizontal blanking)
	pixels := make([][]byte, h)
	for y := range pixels {
		pixels[y] = make([]byte, w)
		for x := range pixels[y] {
			pixels[y][x] = unwritten
		}
	}
	v.WritePixelToBuffer = func(x, y int, colorIndex byte) {
		if y >= 0 && y < h && x >= 0 && x < w {
			pixels[y][x] = colorIndex
		}
	}
	v.Reset()

	v.WriteRegister(0xD020, 0x0E) // border: light blue (14)
	v.WriteRegister(0xD021, 0x06) // background: blue (6)
	v.WriteRegister(0xD011, 0x1B) // DEN=1, RSEL=1 (25 rows), YSCROLL=3
	v.WriteRegister(0xD016, 0x08) // CSEL=1 (40 cols), MCM=0, XSCROLL=0
	v.WriteRegister(0xD018, 0x14) // VM=1 (screen @ $0400), CB=2 (chars @ $1000)

	for col := range 40 {
		ram[0x0400+col] = byte(1 + col) // screen code 1 = 'A'
		ram[0xD800+col] = 0x01
	}

	const dotsPerFrame = DotsPerLine * RasterLinesPerFrame
	for range dotsPerFrame {
		v.StepDot()
	}

	// Raster line 100 is safely inside the 25-row display window
	// (51-250) and outside the very first Bad Line's character row, so
	// c-accesses have long since populated the video matrix buffer.
	row := pixels[100]
	firstNonBorder := -1
	lastNonBorder := -1
	for x, c := range row {
		if c == unwritten {
			continue
		}
		if c != 0x0E {
			if firstNonBorder == -1 {
				firstNonBorder = x
			}
			lastNonBorder = x
		}
	}
	t.Logf("row 100: first non-border pixel at displayX=%d, last at displayX=%d (row width=%d)", firstNonBorder, lastNonBorder, w)

	// Expected: left border/display transition at real X=$18 (24), right
	// at real X=$158 (344), remapped into displayX via
	// (rasterX - firstVisXCoo + DotsPerLine) % DotsPerLine.
	wantFirst := (24 - firstVisXCoo + DotsPerLine) % DotsPerLine
	wantLast := (344-1-firstVisXCoo+DotsPerLine)%DotsPerLine - 1 // last border pixel is one before the right comparison value
	t.Logf("want first non-border displayX=%d", wantFirst)

	if firstNonBorder != wantFirst {
		t.Errorf("first non-border pixel at displayX=%d, want %d", firstNonBorder, wantFirst)
	}
	_ = wantLast
}

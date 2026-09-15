//go:build !tinygo && !headless

package tiny64

import (
	"bytes"
	"testing"
)

func saveFrameBuffers(t *testing.T) {
	t.Helper()
	indexed, rgba := frameBufferIndexed, frameBufferRGBAExpanded
	t.Cleanup(func() {
		frameBufferIndexed, frameBufferRGBAExpanded = indexed, rgba
	})
}

func TestFrameBufferIndexedLayout(t *testing.T) {
	saveFrameBuffers(t)
	if FrameBufferStride != 408 || FrameBufferStride%4 != 0 {
		t.Fatalf("indexed stride = %d, want 408 and divisible by four", FrameBufferStride)
	}
	fb := FrameBufferIndexed()
	if len(fb) != 115872 {
		t.Fatalf("indexed visible length = %d, want 115872", len(fb))
	}
	for i := range frameBufferIndexed {
		frameBufferIndexed[i] = 0xff
	}
	for y := range VisibleLines {
		selectPixelRow(uint16(y + FirstVisibleLine))
		for x := range VisibleDotsPerLine {
			writePixelToRow(uint16(x), 0xf0|byte((x+y)%16))
		}
	}
	for y := range RasterLinesPerFrame {
		for x := range FrameBufferStride {
			want := byte(0xff)
			if y >= FirstVisibleLine && y < FirstVisibleLine+VisibleLines && x < VisibleDotsPerLine {
				want = byte((x + y - FirstVisibleLine) % 16)
				texel, lane := x/4, x%4
				if got := fb[(y-FirstVisibleLine)*FrameBufferStride+texel*4+lane]; got != want {
					t.Fatalf("texel lane at (%d,%d) = %d, want %d", x, y, got, want)
				}
			}
			if got := frameBufferIndexed[y*FrameBufferStride+x]; got != want {
				t.Fatalf("raster storage at (%d,%d) = %d, want %d", x, y, got, want)
			}
		}
	}

	rgba := FrameBufferRGBA()
	if len(rgba) != 460080 {
		t.Fatalf("RGBA length = %d, want 460080", len(rgba))
	}
	for y := range VisibleLines {
		for x := range VisibleDotsPerLine {
			i := (y*VisibleDotsPerLine + x) * 4
			if got, want := [4]byte(rgba[i:i+4]), C64Palette[(x+y)%16]; got != want {
				t.Fatalf("RGBA at (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
}

func TestFrameBufferRGBALifetime(t *testing.T) {
	saveFrameBuffers(t)
	ClearFrameBuffer()
	indexed := FrameBufferIndexed()
	selectPixelRow(FirstVisibleLine)
	writePixelToRow(0, 2)
	rgba := FrameBufferRGBA()
	retained := bytes.Clone(rgba)

	writePixelToRow(0, 3)
	if indexed[0] != 3 {
		t.Fatalf("indexed view did not update: got %d, want 3", indexed[0])
	}
	if got := [4]byte(rgba[:4]); got != C64Palette[2] {
		t.Fatalf("emulation changed RGBA snapshot: got %v", got)
	}
	next := FrameBufferRGBA()
	if &next[0] != &rgba[0] {
		t.Fatal("RGBA conversion did not reuse its buffer")
	}
	if got := [4]byte(rgba[:4]); got != C64Palette[3] {
		t.Fatalf("conversion did not refresh RGBA snapshot: got %v", got)
	}
	if got := [4]byte(retained[:4]); got != C64Palette[2] {
		t.Fatalf("conversion changed caller's copy: got %v", got)
	}
}

func TestClearFrameBuffer(t *testing.T) {
	saveFrameBuffers(t)
	for i := range frameBufferIndexed {
		frameBufferIndexed[i] = 0xff
	}
	ClearFrameBuffer()
	for i, got := range frameBufferIndexed {
		if got != 0 {
			t.Fatalf("indexed byte %d after clear = %d, want 0", i, got)
		}
	}
	rgba := FrameBufferRGBA()
	for i := 0; i < len(rgba); i += 4 {
		if got := [4]byte(rgba[i : i+4]); got != C64Palette[0] {
			t.Fatalf("cleared RGBA pixel %d = %v, want palette black", i/4, got)
		}
	}
}

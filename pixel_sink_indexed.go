//go:build !tinygo && !headless && !pixelsink_func

package tiny64

// This sink stores what the VIC-II actually produces: a four bit colour
// index per pixel. Expanding those indices into RGBA is the display's job,
// and on the desktop the GPU does it (see cmd/internal/desktop), so the
// frame buffer is a quarter the size of an RGBA one and a pixel write is a
// single byte store rather than a four byte copy.
//
// The frame buffer row stride is padded from 405 (VisibleDotsPerLine) to
// 408 (FrameBufferStride, 102 texels * 4 channels) so each raster line is
// already aligned to whole RGBA texels. The three padding bytes per line
// fall in the horizontal blanking interval and are never written by the
// VIC-II, allowing the GPU texture to be updated with a single direct
// WritePixels call without any per-frame CPU packing or line expansion.

const (
	FrameBufferStride  = (VisibleDotsPerLine + 3) &^ 3
	visibleFrameOffset = FirstVisibleLine * FrameBufferStride
)

var frameBufferIndexed [FrameBufferStride * RasterLinesPerFrame]byte

// FrameBufferIndexed returns the current visible frame in row-major order,
// one C64Palette index per pixel, padded to FrameBufferStride bytes per line.
func FrameBufferIndexed() []byte {
	return frameBufferIndexed[visibleFrameOffset : visibleFrameOffset+FrameBufferStride*VisibleLines]
}

// frameBufferRGBAExpanded backs FrameBufferRGBA. It exists for the callers
// that still want whole pixels on the CPU — the PNG snapshot tool and the
// tests — and is deliberately not touched by the emulation itself.
var frameBufferRGBAExpanded [VisibleDotsPerLine * VisibleLines * 4]byte

// FrameBufferRGBA expands the current visible frame into row-major RGBA
// order. Unlike the RGBA sink's, this buffer is a snapshot taken at the
// moment of the call, not a live view of the frame being drawn.
func FrameBufferRGBA() []byte {
	src := FrameBufferIndexed()
	for y := range VisibleLines {
		srcRow := src[y*FrameBufferStride : y*FrameBufferStride+VisibleDotsPerLine]
		dstRow := frameBufferRGBAExpanded[y*VisibleDotsPerLine*4 : (y+1)*VisibleDotsPerLine*4]
		for x, colorIndex := range srcRow {
			copy(dstRow[x*4:x*4+4], C64Palette[colorIndex&0x0f][:])
		}
	}
	return frameBufferRGBAExpanded[:]
}

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {
	clear(frameBufferIndexed[:])
}

func writePixelToBuffer(x, y uint16, colorIndex byte) {
	frameBufferIndexed[int(y)*FrameBufferStride+int(x)] = colorIndex & 0x0f
}

//go:build !tinygo && !headless && !pixelsink_func

package tiny64

// This sink stores what the VIC-II actually produces: a four bit colour
// index per pixel. Expanding those indices into RGBA is the display's job,
// and on the desktop the GPU does it (see cmd/internal/desktop), so the
// frame buffer is a quarter the size of a RGBA one and a pixel write is a
// single byte store rather than a four byte copy.

const visibleFrameOffset = FirstVisibleLine * VisibleDotsPerLine

var frameBufferIndexed [VisibleDotsPerLine * RasterLinesPerFrame]byte

// FrameBufferIndexed returns the current visible frame in row-major order,
// one C64Palette index per pixel.
func FrameBufferIndexed() []byte {
	return frameBufferIndexed[visibleFrameOffset : visibleFrameOffset+VisibleDotsPerLine*VisibleLines]
}

// frameBufferRGBAExpanded backs FrameBufferRGBA. It exists for the callers
// that still want whole pixels on the CPU — the PNG snapshot tool and the
// tests — and is deliberately not touched by the emulation itself.
var frameBufferRGBAExpanded [VisibleDotsPerLine * VisibleLines * 4]byte

// FrameBufferRGBA expands the current visible frame into row-major RGBA
// order. Unlike the RGBA sink's, this buffer is a snapshot taken at the
// moment of the call, not a live view of the frame being drawn.
func FrameBufferRGBA() []byte {
	for i, colorIndex := range FrameBufferIndexed() {
		copy(frameBufferRGBAExpanded[i*4:i*4+4], C64Palette[colorIndex&0x0f][:])
	}
	return frameBufferRGBAExpanded[:]
}

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {
	clear(frameBufferIndexed[:])
}

func writePixelToBuffer(x, y uint16, colorIndex byte) {
	frameBufferIndexed[int(y)*VisibleDotsPerLine+int(x)] = colorIndex & 0x0f
}

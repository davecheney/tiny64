//go:build !tinygo && !headless

package tiny64

const (
	// FrameBufferStride is the indexed row size in bytes, rounded up to
	// four so the desktop can upload each group as one RGBA texel.
	// Padding is unused storage, not additional visible VIC-II dots.
	FrameBufferStride = (VisibleDotsPerLine + 3) &^ 3

	visibleFrameOffset = FirstVisibleLine * FrameBufferStride
)

var frameBufferIndexed [FrameBufferStride * RasterLinesPerFrame]byte

// FrameBufferIndexed returns the current visible frame in row-major order,
// one C64Palette index per pixel, padded to FrameBufferStride bytes per line.
// The returned slice is a live view of the framebuffer, updated by emulation.
func FrameBufferIndexed() []byte {
	return frameBufferIndexed[visibleFrameOffset : visibleFrameOffset+FrameBufferStride*VisibleLines]
}

var frameBufferRGBAExpanded [VisibleDotsPerLine * VisibleLines * 4]byte

// FrameBufferRGBA expands the current visible frame into row-major RGBA
// order, without row padding. The returned slice uses shared storage:
// emulation does not update it, but the next FrameBufferRGBA call overwrites
// it. Callers retaining a snapshot across calls must copy it.
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

func writePixelToIndexed(x, y uint16, colorIndex byte) {
	frameBufferIndexed[int(y)*FrameBufferStride+int(x)] = colorIndex & 0x0f
}

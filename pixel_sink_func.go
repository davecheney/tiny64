//go:build !tinygo && pixelsink_func

package tiny64

// This sink is the indexed one with the pixel write reached through a
// function value rather than called directly, so the cost of that
// indirection can be measured against it.

const (
	FrameBufferStride  = (VisibleDotsPerLine + 3) &^ 3
	visibleFrameOffset = FirstVisibleLine * FrameBufferStride
)

var frameBufferIndexed [FrameBufferStride * RasterLinesPerFrame]byte

var writePixelToBuffer = writePixelToIndexed

// FrameBufferIndexed returns the current visible frame in row-major order,
// one C64Palette index per pixel, padded to FrameBufferStride bytes per line.
func FrameBufferIndexed() []byte {
	return frameBufferIndexed[visibleFrameOffset : visibleFrameOffset+FrameBufferStride*VisibleLines]
}

var frameBufferRGBAExpanded [VisibleDotsPerLine * VisibleLines * 4]byte

// FrameBufferRGBA expands the current visible frame into row-major RGBA
// order. The buffer is a snapshot taken at the moment of the call, not a
// live view of the frame being drawn.
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

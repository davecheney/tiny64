//go:build !tinygo && pixelsink_func

package tiny64

const visibleFrameOffset = FirstVisibleLine * VisibleDotsPerLine * 4

var frameBufferRGBA [VisibleDotsPerLine * RasterLinesPerFrame * 4]byte

var writePixelToBuffer = writePixelToRGBA

// FrameBufferRGBA returns the current visible frame in row-major RGBA order.
func FrameBufferRGBA() []byte {
	return frameBufferRGBA[visibleFrameOffset : visibleFrameOffset+VisibleDotsPerLine*VisibleLines*4]
}

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {
	clear(frameBufferRGBA[:])
}

func writePixelToRGBA(x, y uint16, colorIndex byte) {
	const stride = VisibleDotsPerLine * 4
	idx := int(y)*stride + int(x)*4
	copy(frameBufferRGBA[idx:idx+4], C64Palette[colorIndex&0x0f][:])
}

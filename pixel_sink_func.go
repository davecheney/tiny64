//go:build !tinygo && pixelsink_func

package tiny64

const visibleFrameOffset = FirstVisibleLine * VisibleDotsPerLine * 4

var frameBufferRGBA [VisibleDotsPerLine * RasterLinesPerFrame * 4]byte

var writePixelToBuffer = writePixelToRGBA

// FrameBufferRGBA returns the current visible frame in row-major RGBA order.
func FrameBufferRGBA() []byte {
	return frameBufferRGBA[visibleFrameOffset : visibleFrameOffset+VisibleDotsPerLine*VisibleLines*4]
}

func writePixelToRGBA(x, y uint16, colorIndex byte) {
	const stride = VisibleDotsPerLine * 4
	idx := int(y)*stride + int(x)*4
	copy(frameBufferRGBA[idx:idx+4], C64Palette[colorIndex&0x0f][:])
}

func writePixels4ToBuffer(x, y uint16, c0, c1, c2, c3 byte) {
	writePixelToBuffer(x, y, c0)
	writePixelToBuffer(x+1, y, c1)
	writePixelToBuffer(x+2, y, c2)
	writePixelToBuffer(x+3, y, c3)
}

func writePixels2ToBuffer(x, y uint16, c0, c1 byte) {
	writePixelToBuffer(x, y, c0)
	writePixelToBuffer(x+1, y, c1)
}

func writePixelInWindow(x, y uint16, colorIndex byte) {
	writePixelToBuffer(x, y, colorIndex)
}

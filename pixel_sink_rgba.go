//go:build !tinygo && !headless && !pixelsink_func

package tiny64

var frameBufferRGBA [VisibleDotsPerLine * VisibleLines * 4]byte

// FrameBufferRGBA returns the current visible frame in row-major RGBA order.
func FrameBufferRGBA() []byte {
	return frameBufferRGBA[:]
}

func writePixelToBuffer(x, y uint16, colorIndex byte) {
	const stride = VisibleDotsPerLine * 4
	idx := int(y-FirstVisibleLine)*stride + int(x)*4
	copy(frameBufferRGBA[idx:idx+4], C64Palette[colorIndex&0x0f][:])
}

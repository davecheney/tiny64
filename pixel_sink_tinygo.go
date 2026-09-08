//go:build tinygo

package tiny64

const (
	frameBufferWidth  = 320
	frameBufferHeight = 240
	frameBufferX      = 48
	frameBufferY      = 31
)

var frameBufferRGB565BE [frameBufferWidth * frameBufferHeight * 2]byte

// FrameBufferRGB565BE returns the current visible frame in row-major RGB565
// big-endian order.
func FrameBufferRGB565BE() []byte {
	return frameBufferRGB565BE[:]
}

func writePixelToBuffer(x, y uint16, colorIndex byte) {
	x -= frameBufferX
	y -= frameBufferY
	if x >= frameBufferWidth || y >= frameBufferHeight {
		return
	}
	const stride = frameBufferWidth * 2
	idx := int(y)*stride + int(x)*2
	color := c64PaletteRGB565BE[colorIndex&0x0f]
	frameBufferRGB565BE[idx] = color[0]
	frameBufferRGB565BE[idx+1] = color[1]
}

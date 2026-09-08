//go:build tinygo

package tiny64

var c64PaletteRGB565BE = [16][2]byte{
	{0x00, 0x00},
	{0xff, 0xff},
	{0x88, 0x00},
	{0xaf, 0xfd},
	{0xca, 0x39},
	{0x06, 0x6a},
	{0x00, 0x15},
	{0xef, 0x6e},
	{0xdc, 0x4a},
	{0x62, 0x20},
	{0xfb, 0xae},
	{0x31, 0x86},
	{0x73, 0xae},
	{0xaf, 0xec},
	{0x04, 0x5f},
	{0xbd, 0xd7},
}

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

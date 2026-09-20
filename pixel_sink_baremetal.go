//go:build baremetal

// The bare-metal sink writes the picture straight out as big-endian RGB565
// cropped to 320x240, the format and size an ST7789 panel wants, because a
// board has neither the RAM for the full 408x293 indexed frame nor a shader
// to expand it. It is keyed on baremetal, not tinygo: TinyGo compiling for
// the host is still a desktop, and should get the desktop frame buffer.

package tiny64

import "unsafe"

const (
	rgb565Width  = 320
	rgb565Height = 240
	rgb565CropX  = 48
	rgb565CropY  = 31
)

var (
	frameBufferRGB565BE [rgb565Width * rgb565Height]uint16
	c64PaletteRGB565BE  = [16]uint16{
		0x0000, 0xffff, 0x0088, 0xfdaf,
		0x39ca, 0x6a06, 0x1500, 0x6eef,
		0x4adc, 0x2062, 0xaefb, 0x8631,
		0xae73, 0xecaf, 0x5f04, 0xd7bd,
	}
)

// FrameBufferRGB565BE returns the 320x240 cropped frame in RGB565BE byte order.
func FrameBufferRGB565BE() []byte {
	fb := frameBufferRGB565BE[:]
	return unsafe.Slice((*byte)(unsafe.Pointer(&fb[0])), len(fb)*2)
}

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {
	clear(frameBufferRGB565BE[:])
}

// writePixels4ToBuffer writes one Phi0 half-phase's four dots, which is
// the only width the dot path writes in. The crop is asked once for the
// group rather than once a dot: x is a multiple of four, and so are the
// crop's left edge and its width, so all four dots are inside the panel
// or all four are outside.
func writePixels4ToBuffer(x, y uint16, c0, c1, c2, c3 byte) {
	x -= rgb565CropX
	y -= rgb565CropY
	if x >= rgb565Width || y >= rgb565Height {
		return
	}
	i := int(y)*rgb565Width + int(x)
	row := frameBufferRGB565BE[i : i+4]
	row[0] = c64PaletteRGB565BE[c0&0x0f]
	row[1] = c64PaletteRGB565BE[c1&0x0f]
	row[2] = c64PaletteRGB565BE[c2&0x0f]
	row[3] = c64PaletteRGB565BE[c3&0x0f]
}

// The group crop above is only sound while the panel's left edge and
// width are multiples of four, and while a bus cycle's dots divide into
// groups of four. Converting a negative constant to uint is the error.
const (
	_ = uint(0 - rgb565CropX%4)
	_ = uint(0 - rgb565Width%4)
	_ = uint(0 - DotsPerCycle%4)
)

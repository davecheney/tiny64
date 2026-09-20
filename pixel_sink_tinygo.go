//go:build tinygo

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

func writePixelToBuffer(x, y uint16, colorIndex byte) {
	x -= rgb565CropX
	y -= rgb565CropY
	if x >= rgb565Width || y >= rgb565Height {
		return
	}
	frameBufferRGB565BE[int(y)*rgb565Width+int(x)] = c64PaletteRGB565BE[colorIndex&0x0f]
}

// writePixels4ToBuffer writes the four dots of one Phi0 half-phase at
// once. They are consecutive pixels of a single line, so the crop, the
// row offset and the bounds check are done once for the group rather
// than once per dot.
func writePixels4ToBuffer(x, y uint16, c0, c1, c2, c3 byte) {
	x -= rgb565CropX
	y -= rgb565CropY
	if x+3 >= rgb565Width || y >= rgb565Height {
		return
	}
	i := int(y)*rgb565Width + int(x)
	row := frameBufferRGB565BE[i : i+4]
	row[0] = c64PaletteRGB565BE[c0&0x0f]
	row[1] = c64PaletteRGB565BE[c1&0x0f]
	row[2] = c64PaletteRGB565BE[c2&0x0f]
	row[3] = c64PaletteRGB565BE[c3&0x0f]
}

// writePixels2ToBuffer writes the two dots of the second Phi0 half-phase
// group, on the same terms as writePixels4ToBuffer.
func writePixels2ToBuffer(x, y uint16, c0, c1 byte) {
	x -= rgb565CropX
	y -= rgb565CropY
	if x+1 >= rgb565Width || y >= rgb565Height {
		return
	}
	i := int(y)*rgb565Width + int(x)
	row := frameBufferRGB565BE[i : i+2]
	row[0] = c64PaletteRGB565BE[c0&0x0f]
	row[1] = c64PaletteRGB565BE[c1&0x0f]
}

// writePixelInWindow writes a dot the caller has already proved lies on a
// drawable line inside the rendered window, so the crop's own bounds test
// would be answering a question already settled.
func writePixelInWindow(x, y uint16, colorIndex byte) {
	frameBufferRGB565BE[int(y-rgb565CropY)*rgb565Width+int(x-rgb565CropX)] = c64PaletteRGB565BE[colorIndex&0x0f]
}

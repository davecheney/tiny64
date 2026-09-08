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
	return unsafe.Slice((*byte)(unsafe.Pointer(&frameBufferRGB565BE[0])), len(frameBufferRGB565BE)*2)
}

func writePixelToBuffer(x, y uint16, colorIndex byte) {
	x -= rgb565CropX
	y -= rgb565CropY
	if x >= rgb565Width || y >= rgb565Height {
		return
	}
	frameBufferRGB565BE[int(y)*rgb565Width+int(x)] = c64PaletteRGB565BE[colorIndex&0x0f]
}

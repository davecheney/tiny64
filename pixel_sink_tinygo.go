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

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {
	clear(frameBufferRGB565BE[:])
}

// pixelRowRGB565 is the storage for the raster line the beam is on, which
// selectPixelRow last pointed it at, or empty on a line the 320x240 crop
// does not keep. Which row that is depends only on rasterLine, so it goes
// stale exactly when lineVisible and lineDrawable do, and is refreshed in
// the same two places: dotclock7's line wrap, and syncLineVisibility for
// every other write to rasterLine.
var pixelRowRGB565 []uint16

// selectPixelRow points the sink at raster line y, and is where this sink
// applies the vertical half of the crop: an off-window line gets an empty
// row, which writePixelToRow then drops every pixel of. It is the row
// cache's only writer; see pixelRowRGB565 for what invalidates it.
func selectPixelRow(y uint16) {
	y -= rgb565CropY
	if y >= rgb565Height {
		pixelRowRGB565 = nil
		return
	}
	base := int(y) * rgb565Width
	pixelRowRGB565 = frameBufferRGB565BE[base : base+rgb565Width]
}

// writePixelToRow applies the horizontal half of the crop. Unlike the
// vertical half this cannot move to selectPixelRow, and is not redundant
// with the callers' render window test: finishSideBorder paints the right
// border out to VisibleDotsPerLine (405), past renderDotAfter (368) on
// this target, and expects those dots to be dropped rather than to fault.
// Testing x against the row's length rather than rgb565Width lets the
// compiler drop the bounds check on the store that follows.
func writePixelToRow(x uint16, colorIndex byte) {
	row := pixelRowRGB565
	x -= rgb565CropX
	if int(x) >= len(row) {
		return
	}
	row[x] = c64PaletteRGB565BE[colorIndex&0x0f]
}

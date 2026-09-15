//go:build !tinygo && !headless

package tiny64

const (
	// FrameBufferStride is the indexed row size in bytes, rounded up to
	// four so the desktop can upload each group as one RGBA texel.
	// Padding is unused storage, not additional visible VIC-II dots.
	FrameBufferStride = (VisibleDotsPerLine + 3) &^ 3

	visibleFrameOffset = FirstVisibleLine * FrameBufferStride
)

var frameBufferIndexed [FrameBufferStride * RasterLinesPerFrame]byte

// FrameBufferIndexed returns the current visible frame in row-major order,
// one C64Palette index per pixel, padded to FrameBufferStride bytes per line.
// The returned slice is a live view of the framebuffer, updated by emulation.
func FrameBufferIndexed() []byte {
	return frameBufferIndexed[visibleFrameOffset : visibleFrameOffset+FrameBufferStride*VisibleLines]
}

var frameBufferRGBAExpanded [VisibleDotsPerLine * VisibleLines * 4]byte

// FrameBufferRGBA expands the current visible frame into row-major RGBA
// order, without row padding. The returned slice uses shared storage:
// emulation does not update it, but the next FrameBufferRGBA call overwrites
// it. Callers retaining a snapshot across calls must copy it.
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

// pixelRowIndexed is the storage for the raster line the beam is on,
// which selectPixelRow last pointed it at. Which row that is depends only
// on rasterLine, so it goes stale exactly when lineVisible and
// lineDrawable do, and is refreshed in the same two places: dotclock7's
// line wrap, and syncLineVisibility for every other write to rasterLine.
// Holding the row means a pixel write is an index into it rather than a
// multiply from the base of the frame, on every one of the ~115,000
// pixels a frame paints.
var pixelRowIndexed []byte

// selectPixelRow points the sink at raster line y. It is the row cache's
// only writer; see pixelRowIndexed for what invalidates it.
func selectPixelRow(y uint16) {
	base := int(y) * FrameBufferStride
	pixelRowIndexed = frameBufferIndexed[base : base+VisibleDotsPerLine]
}

func writePixelToIndexedRow(x uint16, colorIndex byte) {
	pixelRowIndexed[x] = colorIndex & 0x0f
}

//go:build !baremetal && !headless

package tiny64

const (
	// FrameBufferStride is the indexed row size in bytes: one palette
	// index per visible VIC-II dot, with no padding between rows.
	FrameBufferStride = VisibleDotsPerLine

	visibleFrameOffset = FirstVisibleLine * FrameBufferStride
)

var frameBufferIndexed [FrameBufferStride * RasterLinesPerFrame]byte

// FrameBufferIndexed returns the current visible frame in row-major order,
// one C64Palette index per pixel, FrameBufferStride bytes per line. The
// returned slice is a live view of the framebuffer, updated by emulation.
func FrameBufferIndexed() []byte {
	return frameBufferIndexed[visibleFrameOffset : visibleFrameOffset+FrameBufferStride*VisibleLines]
}

var frameBufferRGBAExpanded [VisibleDotsPerLine * VisibleLines * 4]byte

// FrameBufferRGBA expands the current visible frame into row-major RGBA
// order, without row padding. The returned slice uses shared storage:
// emulation does not update it, but the next FrameBufferRGBA call overwrites
// it. Callers retaining a snapshot across calls must copy it.
//
// Nothing in the emulator's own display path expands a frame any more.
// The desktop hands the GPU one palette index per pixel and lets it do
// the lookup, so the only frames that become colour on the CPU are the
// ones something wants the bytes of: PNG capture, and the tests.
//
// This used to take the destination and its row pitch as arguments, so
// that a display backend could expand straight into a driver's own
// buffer and save the copy out of ours. Nothing needs that now, and a
// parameter no caller varies is a parameter that only invites getting it
// wrong.
func FrameBufferRGBA() []byte {
	const pitch = VisibleDotsPerLine * 4

	src := FrameBufferIndexed()
	dst := frameBufferRGBAExpanded[:]
	for y := range VisibleLines {
		srcRow := src[y*FrameBufferStride : y*FrameBufferStride+VisibleDotsPerLine]
		dstRow := dst[y*pitch : y*pitch+pitch]
		for x, colorIndex := range srcRow {
			copy(dstRow[x*4:x*4+4], C64Palette[colorIndex&0x0f][:])
		}
	}
	return dst
}

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {
	clear(frameBufferIndexed[:])
}

// writePixels4ToBuffer writes one Phi0 half-phase's four dots at once:
// four consecutive pixels of one line, so one row offset and one bounds
// check instead of four of each.
func writePixels4ToBuffer(x, y uint16, c0, c1, c2, c3 byte) {
	i := int(y)*FrameBufferStride + int(x)
	row := frameBufferIndexed[i : i+4]
	row[0] = c0 & 0x0f
	row[1] = c1 & 0x0f
	row[2] = c2 & 0x0f
	row[3] = c3 & 0x0f
}

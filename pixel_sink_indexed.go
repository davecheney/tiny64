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
// This is the form callers want when they want the bytes themselves -
// PNG capture, and the tests. A caller with storage of its own to fill
// can use ExpandFrameBufferRGBA and skip the copy.
func FrameBufferRGBA() []byte {
	ExpandFrameBufferRGBA(frameBufferRGBAExpanded[:], VisibleDotsPerLine*4)
	return frameBufferRGBAExpanded[:]
}

// ExpandFrameBufferRGBA expands the current visible frame into dst as
// row-major RGBA, starting each row pitch bytes after the last. dst must
// hold VisibleLines rows of that pitch, and pitch must be at least
// VisibleDotsPerLine*4.
//
// The destination and its pitch are parameters so that a caller can
// expand straight into storage it already has, of whatever row length
// that storage uses, rather than into a buffer of ours that then has to
// be copied out of.
//
// Nothing in the emulator's own display path needs this any more: the
// desktop hands the GPU one palette index per pixel and lets it do the
// lookup, so the only frames expanded to colour on the CPU are the ones
// something wants the bytes of - PNG capture and the tests, both of which
// come through FrameBufferRGBA.
func ExpandFrameBufferRGBA(dst []byte, pitch int) {
	src := FrameBufferIndexed()
	for y := range VisibleLines {
		srcRow := src[y*FrameBufferStride : y*FrameBufferStride+VisibleDotsPerLine]
		dstRow := dst[y*pitch : y*pitch+VisibleDotsPerLine*4]
		for x, colorIndex := range srcRow {
			copy(dstRow[x*4:x*4+4], C64Palette[colorIndex&0x0f][:])
		}
	}
}

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {
	clear(frameBufferIndexed[:])
}

func writePixelToIndexed(x, y uint16, colorIndex byte) {
	frameBufferIndexed[int(y)*FrameBufferStride+int(x)] = colorIndex & 0x0f
}

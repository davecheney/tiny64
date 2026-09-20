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
// A caller that is going to hand the result straight to a graphics API
// should use ExpandFrameBufferRGBA instead, and expand into whatever
// buffer that API is going to read from. This one exists for callers that
// want the bytes themselves - PNG capture, and the tests.
func FrameBufferRGBA() []byte {
	ExpandFrameBufferRGBA(frameBufferRGBAExpanded[:], VisibleDotsPerLine*4)
	return frameBufferRGBAExpanded[:]
}

// ExpandFrameBufferRGBA expands the current visible frame into dst as
// row-major RGBA, starting each row pitch bytes after the last. dst must
// hold VisibleLines rows of that pitch, and pitch must be at least
// VisibleDotsPerLine*4.
//
// Taking the destination as a parameter is what lets a display backend
// expand once instead of twice. SDL hands out the texture's own staging
// buffer through SDL_LockTexture, so expanding into that writes the frame
// where the driver is already going to read it; expanding into storage of
// our own and then asking SDL to copy it there costs a second pass over
// every pixel, which at 408x293 and 60Hz is about 57MB/s of memory
// traffic for nothing. The pitch is a parameter for the same reason: it
// is the driver's, not ours, and it need not be a packed row.
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

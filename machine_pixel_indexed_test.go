//go:build !baremetal && !headless

// The desktop half of the test suite's pixel accessor. It pairs with
// machine_pixel_baremetal_test.go the same way pixel_sink_indexed.go pairs
// with pixel_sink_baremetal.go: every visual test reads the picture through
// frameBufferPixelRGBA, and only this one function knows which sink is
// underneath. Without the pair, the whole root test package fails to compile
// under baremetal on the first line that names FrameBufferStride, which is
// what kept that configuration at zero test coverage.

package tiny64

// frameIsCropped reports whether the sink under these tests keeps less
// than the whole visible picture. The desktop sink keeps all of it.
const frameIsCropped = false

// frameBufferPixelRGBA reads one dot of the finished frame as a palette
// colour, addressing the indexed buffer the desktop sink writes.
func frameBufferPixelRGBA(x, y uint16) [4]byte {
	idx := int(y-FirstVisibleLine)*FrameBufferStride + int(x)
	return C64Palette[FrameBufferIndexed()[idx]&0x0f]
}

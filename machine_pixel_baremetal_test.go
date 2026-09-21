//go:build baremetal

// The bare-metal half of the test suite's pixel accessor. It pairs with
// machine_pixel_indexed_test.go: every visual test reads the picture through
// frameBufferPixelRGBA, and only these two files know which sink is
// underneath. Without this half the whole root test package fails to compile
// under baremetal on the first line that names FrameBufferStride, which is
// what kept that configuration at zero test coverage.

package tiny64

// frameIsCropped reports whether the sink under these tests keeps less
// than the whole visible picture. This one keeps a 320x240 window of it,
// so a test that asserts on the side borders, or on any dot left of
// rgb565CropX, is asking about pixels that were never stored. Those tests
// skip on this rather than being deleted: they are still the coverage the
// desktop sink gets.
const frameIsCropped = true

// offPanel is what a dot outside the panel reads as. The bare-metal sink
// crops, so the frame simply does not contain the side borders or the top
// and bottom of the picture, and there is no colour to return. A zero
// alpha matches no entry in C64Palette - every one of its sixteen is
// opaque - so a test that reaches outside the crop fails on the colour
// rather than passing on a plausible-looking black.
var offPanel = [4]byte{}

// frameBufferPixelRGBA reads one dot of the finished frame as a palette
// colour, mapping the bare-metal sink's RGB565 back to the index that
// produced it.
//
// The sink converts to RGB565 on the way in, so unlike the indexed buffer
// there is no index left to read. Going back through c64PaletteRGB565BE
// recovers it: the sixteen entries are distinct, so the match is unique.
func frameBufferPixelRGBA(x, y uint16) [4]byte {
	x -= rgb565CropX
	y -= rgb565CropY
	if x >= rgb565Width || y >= rgb565Height {
		return offPanel
	}

	pixel := frameBufferRGB565BE[int(y)*rgb565Width+int(x)]
	for i, c := range c64PaletteRGB565BE {
		if c == pixel {
			return C64Palette[i]
		}
	}

	// The sink only ever writes palette entries, so reaching here means
	// something other than the dot path wrote to the frame.
	return offPanel
}

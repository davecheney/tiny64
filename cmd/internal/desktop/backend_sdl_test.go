//go:build sdl

package desktop

import "testing"

// TestPixelFormatMatchesPalette runs the same check openDisplay makes
// before it creates the texture, without needing a video subsystem, a
// window or a GPU to do it.
func TestPixelFormatMatchesPalette(t *testing.T) {
	if err := checkPixelFormat(); err != nil {
		t.Fatal(err)
	}
}

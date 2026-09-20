package desktop

import (
	"testing"

	"github.com/davecheney/tiny64"
)

// The SDL2 backend checked at startup that SDL_PIXELFORMAT_RGBA32 really
// laid a pixel out as R, G, B, A in ascending address order, because the
// frame was uploaded as expanded colour and a channel-swapped picture
// would otherwise have gone unnoticed. The paletted texture retires that
// assumption rather than needing it checked: colours reach SDL as
// SDL_Color fields, by name, so there is no byte order to get wrong.
//
// What replaces it are the two things the upload now assumes. Neither
// needs a video subsystem, a window or a GPU. (They are also all that can
// be checked from here: a test file cannot use cgo, so nothing in this
// package's C surface is reachable from one.)

// TestPaletteFitsIndices checks every index a frame can hold has a colour
// behind it. The VIC-II sink masks what it writes to four bits, so the
// palette must be at least sixteen entries or a legal frame could index
// past the end of it on the GPU.
func TestPaletteFitsIndices(t *testing.T) {
	if got := len(tiny64.C64Palette); got != 16 {
		t.Fatalf("C64Palette has %d colours, want 16 for a four bit index", got)
	}
}

// TestFrameFitsTexture checks the indexed frame buffer is exactly the
// picture the texture is created at, one byte per pixel. present relies on
// that: it copies rows straight across, so a stride disagreeing with the
// texture's width would shear the picture rather than fail outright.
func TestFrameFitsTexture(t *testing.T) {
	if tiny64.FrameBufferStride != ScreenWidth {
		t.Errorf("frame buffer stride = %d, want %d to match the texture width",
			tiny64.FrameBufferStride, ScreenWidth)
	}
	if got, want := len(tiny64.FrameBufferIndexed()), ScreenWidth*ScreenHeight; got != want {
		t.Errorf("indexed frame is %d bytes, want %d for a %dx%d INDEX8 texture",
			got, want, ScreenWidth, ScreenHeight)
	}
}

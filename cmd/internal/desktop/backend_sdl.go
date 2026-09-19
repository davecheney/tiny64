//go:build sdl

// The SDL2 backend, which is what TinyGo can build: Ebitengine reaches
// TinyGo through purego, whose func.go calls reflect.Value.SetPointer, and
// TinyGo's reflect does not implement it.
//
// It is a hand-rolled cgo shim over the dozen SDL calls this needs rather
// than a binding module, because TinyGo's cgo is its own reimplementation
// and the smaller the surface the less of it has to hold. Three of its
// limits shaped what follows, all found by building the thing:
//
//   - "#cgo pkg-config:" is rejected outright, so the flags below are
//     spelled out. Nonexistent -I and -L directories are ignored, which is
//     what lets one unconstrained pair of lines cover both Homebrew's
//     prefix and a Linux distribution's.
//   - Build constraints on a #cgo line ("#cgo darwin CFLAGS:") are
//     rejected too, hence unconstrained rather than per-GOOS.
//   - SDL_DISABLE_ARM_NEON_H is SDL's own escape hatch for exactly this:
//     SDL_cpuinfo.h pulls in <arm_neon.h> on ARM, and TinyGo's bundled
//     clang headers do not include it. Nothing here uses NEON intrinsics.

package desktop

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/davecheney/tiny64"
)

// SDL_MAIN_HANDLED keeps SDL from redefining main to its own entry point,
// which is what lets this link against plain -lSDL2 with no -lSDL2main and
// no C main of our own. SDL_SetMainReady below is the other half of that
// bargain.

// #cgo CFLAGS: -I/opt/homebrew/include -I/usr/local/include -D_THREAD_SAFE -DSDL_DISABLE_ARM_NEON_H
// #cgo LDFLAGS: -L/opt/homebrew/lib -L/usr/local/lib -lSDL2
// #define SDL_MAIN_HANDLED
// #include <SDL2/SDL.h>
import "C"

// sdlEventSize is sizeof(SDL_Event). Events are polled into a buffer of
// that size rather than a C.SDL_Event so the union is never given a Go
// type: unions are the corner of cgo where TinyGo's reimplementation is
// least like the real one, and the only field this needs is the leading
// Uint32 type tag.
//
// TinyGo's cgo has no C.sizeof_SDL_Event, so the 56 SDL pads the union to
// is written out and checked against the header instead: the two arrays
// below have length zero when the constant is right and a length that
// underflows when it is not, so a short buffer that SDL_PollEvent could
// write past the end of fails the build rather than the run.
const sdlEventSize = 56

var (
	_ [sdlEventSize - unsafe.Sizeof(C.SDL_Event{})]byte
	_ [unsafe.Sizeof(C.SDL_Event{}) - sdlEventSize]byte
)

var sdl struct {
	window   *C.SDL_Window
	renderer *C.SDL_Renderer
	frame    *C.SDL_Texture
}

// sdlError wraps whatever SDL last complained about. SDL_GetError returns
// a pointer into SDL's own storage that the next call may overwrite, so
// the string is copied out here rather than held.
func sdlError(what string) error {
	return fmt.Errorf("%s: %s", what, C.GoString(C.SDL_GetError()))
}

// openDisplay brings up the window, the renderer and the streaming texture
// the frame is uploaded into.
func openDisplay(title string) (func(), error) {
	// SDL wants its window and event calls on the thread that set the
	// video mode, and on macOS that must be the main thread. Run is
	// called from main, so pinning here is enough to keep the loop there.
	runtime.LockOSThread()

	C.SDL_SetMainReady()
	if C.SDL_Init(C.SDL_INIT_VIDEO) != 0 {
		return nil, sdlError("SDL_Init")
	}

	closeDisplay := func() {
		// Each of these tolerates nil, so one teardown path serves
		// however far the setup below got.
		C.SDL_DestroyTexture(sdl.frame)
		C.SDL_DestroyRenderer(sdl.renderer)
		C.SDL_DestroyWindow(sdl.window)
		C.SDL_Quit()
	}

	cTitle := C.CString(title)
	defer C.free(unsafe.Pointer(cTitle))

	sdl.window = C.SDL_CreateWindow(cTitle,
		C.SDL_WINDOWPOS_CENTERED, C.SDL_WINDOWPOS_CENTERED,
		ScreenWidth*Scale, ScreenHeight*Scale,
		C.SDL_WINDOW_SHOWN|C.SDL_WINDOW_RESIZABLE)
	if sdl.window == nil {
		closeDisplay()
		return nil, sdlError("SDL_CreateWindow")
	}
	// Don't let the window shrink below the native picture, where the
	// scaling would start throwing away scanlines.
	C.SDL_SetWindowMinimumSize(sdl.window, ScreenWidth, ScreenHeight)

	sdl.renderer = C.SDL_CreateRenderer(sdl.window, -1,
		C.SDL_RENDERER_ACCELERATED|C.SDL_RENDERER_PRESENTVSYNC)
	if sdl.renderer == nil {
		closeDisplay()
		return nil, sdlError("SDL_CreateRenderer")
	}

	// A logical size does the job Layout does for Ebitengine: SDL scales
	// the picture to fill whatever the window currently is, keeps its
	// proportions and letterboxes the remainder.
	//
	// Unlike the Ebitengine backend there is no aspect snap to go with
	// it. snapWindowToPicture exists because Ebitengine exposes neither
	// GLFW's aspect ratio hint nor a resize-finished event; SDL2 has no
	// aspect hint either (SDL_SetWindowAspectRatio is SDL3), so rather
	// than reimplement the settle timer this leaves the window where the
	// user put it and lives with the letterbox.
	if C.SDL_RenderSetLogicalSize(sdl.renderer, ScreenWidth, ScreenHeight) != 0 {
		closeDisplay()
		return nil, sdlError("SDL_RenderSetLogicalSize")
	}

	// Confirm RGBA32 really is byte-order RGBA here before anything
	// depends on it, so a channel-swapped picture reports itself.
	if err := checkPixelFormat(); err != nil {
		closeDisplay()
		return nil, err
	}

	// Streaming access because the whole texture is rewritten every
	// frame, which is the access pattern SDL_UpdateTexture is for.
	sdl.frame = C.SDL_CreateTexture(sdl.renderer,
		C.SDL_PIXELFORMAT_RGBA32, C.SDL_TEXTUREACCESS_STREAMING,
		ScreenWidth, ScreenHeight)
	if sdl.frame == nil {
		closeDisplay()
		return nil, sdlError("SDL_CreateTexture")
	}
	// The picture is opaque and covers the target completely, so there is
	// nothing to blend with. This matches the Ebitengine path's BlendCopy.
	C.SDL_SetTextureBlendMode(sdl.frame, C.SDL_BLENDMODE_NONE)

	return closeDisplay, nil
}

// runLoop steps the machine and presents a frame until the window closes.
// The renderer was created with PRESENTVSYNC, so the display's refresh is
// what paces it.
func runLoop() error {
	for {
		if pumpEvents() {
			return nil
		}

		step()

		if err := present(); err != nil {
			return err
		}
	}
}

// pumpEvents drains SDL's event queue, reporting whether the user asked to
// quit. Key state is not read from here: pollKeyboard samples the keyboard
// as a whole once per frame, but SDL only updates that array while events
// are being pumped, so this has to run regardless.
func pumpEvents() bool {
	var event [sdlEventSize]byte
	for C.SDL_PollEvent((*C.SDL_Event)(unsafe.Pointer(&event[0]))) != 0 {
		// The type tag is the first Uint32 of every event in the union.
		switch *(*uint32)(unsafe.Pointer(&event[0])) {
		case C.SDL_QUIT:
			return true
		}
	}
	return false
}

// checkPixelFormat verifies the claim the frame upload rests on: that
// SDL_PIXELFORMAT_RGBA32 lays a pixel out as the bytes R, G, B, A in
// ascending address order, which is exactly how tiny64.C64Palette stores a
// colour. That is what lets FrameBufferRGBA go straight to
// SDL_UpdateTexture with no repacking in between.
//
// RGBA32 is an alias that resolves to RGBA8888 or ABGR8888 depending on
// the host's endianness, so this asks SDL for the masks rather than
// assuming either. Checking costs one call at startup and turns a silently
// channel-swapped picture into an error naming the cause.
func checkPixelFormat() error {
	var (
		bpp                        C.int
		rMask, gMask, bMask, aMask C.Uint32
	)
	if C.SDL_PixelFormatEnumToMasks(C.SDL_PIXELFORMAT_RGBA32, &bpp, &rMask, &gMask, &bMask, &aMask) == C.SDL_FALSE {
		return sdlError("SDL_PixelFormatEnumToMasks")
	}
	if bpp != 32 {
		return fmt.Errorf("SDL_PIXELFORMAT_RGBA32 is %d bits per pixel, want 32", bpp)
	}

	// Assemble the pixel a palette entry's four bytes make when read back
	// as one native uint32, then check each mask selects the channel
	// those bytes were written for.
	for i, want := range tiny64.C64Palette {
		var pixel uint32
		copy((*[4]byte)(unsafe.Pointer(&pixel))[:], want[:])

		got := [4]byte{
			maskedChannel(pixel, uint32(rMask)),
			maskedChannel(pixel, uint32(gMask)),
			maskedChannel(pixel, uint32(bMask)),
			maskedChannel(pixel, uint32(aMask)),
		}
		if got != want {
			return fmt.Errorf("SDL_PIXELFORMAT_RGBA32 is not byte-order RGBA here: colour %d round-tripped as %v, want %v", i, got, want)
		}
	}
	return nil
}

// maskedChannel extracts the byte a channel mask selects from a pixel.
func maskedChannel(pixel, mask uint32) byte {
	for mask != 0 && mask&1 == 0 {
		mask >>= 1
		pixel >>= 1
	}
	return byte(pixel & mask)
}

// present uploads the frame the VIC-II just finished and puts it on screen.
func present() error {
	// The texture is STREAMING, so SDL will hand over its own staging
	// buffer rather than take a copy of ours. Expanding the frame
	// directly into that writes every pixel once, where SDL_UpdateTexture
	// would have us write them into storage of our own and then have SDL
	// copy them again.
	//
	// SDL owns this pointer until SDL_UnlockTexture, and it is not Go
	// memory, so nothing here can be moved by the collector. The slice is
	// built over it only for the length of the expansion.
	var raw unsafe.Pointer
	var pitch C.int
	if C.SDL_LockTexture(sdl.frame, nil, &raw, &pitch) != 0 {
		return sdlError("SDL_LockTexture")
	}
	tiny64.ExpandFrameBufferRGBA(
		unsafe.Slice((*byte)(raw), int(pitch)*ScreenHeight), int(pitch))
	C.SDL_UnlockTexture(sdl.frame)
	// The letterbox around the picture is SDL's to paint, and it is the
	// only thing the clear is for.
	if C.SDL_RenderClear(sdl.renderer) != 0 {
		return sdlError("SDL_RenderClear")
	}
	if C.SDL_RenderCopy(sdl.renderer, sdl.frame, nil, nil) != 0 {
		return sdlError("SDL_RenderCopy")
	}
	C.SDL_RenderPresent(sdl.renderer)
	return nil
}

// The SDL3 backend.
//
// It is a hand-rolled cgo shim over the dozen SDL calls this needs rather
// than a binding module, because TinyGo's cgo is its own reimplementation
// and the smaller the surface the less of it has to hold. Four of its
// limits shaped what follows, all found by building the thing:
//
//   - "#cgo pkg-config:" is rejected outright, so the flags below are
//     spelled out. Nonexistent -I and -L directories are ignored, which is
//     what lets one unconstrained pair of lines cover both Homebrew's
//     prefix and a Linux distribution's.
//   - Build constraints on a #cgo line ("#cgo darwin CFLAGS:") are
//     rejected too, hence unconstrained rather than per-GOOS.
//   - The include path names SDL's own directory, so the include below
//     can be <SDL.h>. That matters for TinyGo specifically: its bundled
//     clang does not search /usr/include, and putting the whole of it on
//     the path pulls glibc's headers in ahead of clang's own, at which
//     point <wchar.h> cannot find __gnuc_va_list and nothing compiles.
//     Naming .../SDL3 directly reaches SDL.h without disturbing that
//     ordering, which is why /usr/include is absent below.
//     SDL's own headers, however, include each other as <SDL3/SDL_foo.h>,
//     so the parent has to be reachable as well or SDL.h fails on its
//     first line. On Linux it already is: /usr/include is a default
//     search path for the compilers that use it. On macOS Homebrew's
//     prefix is not, so the parents are named too, without naming
//     /usr/include and reintroducing the ordering problem above.
//     The -L list is spelled out because lld does not search /usr/lib
//     either; the multiarch entries are for Debian and Ubuntu, where that
//     is where libSDL3 lands.
//   - SDL_DISABLE_ARM_NEON_H is SDL's own escape hatch for exactly this:
//     SDL_cpuinfo.h pulls in <arm_neon.h> on ARM, and TinyGo's bundled
//     clang headers do not include it. Nothing here uses NEON intrinsics.
//
// SDL3 rather than SDL2 for one reason: SDL_SetTexturePalette, added in
// 3.4.0, lets a texture hold one palette index per pixel and have the GPU
// look the colour up. The VIC-II decides on an index per pixel, so that is
// already the shape of the data, and the frame reaches the GPU as the
// 119,544 bytes it is rather than the 478,176 it would expand to.

package desktop

import (
	"fmt"
	"runtime"
	"time"
	"unsafe"

	"github.com/davecheney/tiny64"
)

// SDL_MAIN_HANDLED keeps SDL from redefining main to its own entry point,
// which is what lets this link against plain -lSDL3 with no SDL_main and
// no C main of our own. SDL_SetMainReady below is the other half of that
// bargain, and in SDL3 it needs SDL_main.h named explicitly: SDL.h used to
// include it and deliberately no longer does.

// #cgo CFLAGS: -I/opt/homebrew/include/SDL3 -I/usr/local/include/SDL3 -I/usr/include/SDL3 -I/opt/homebrew/include -I/usr/local/include -D_THREAD_SAFE -DSDL_DISABLE_ARM_NEON_H
// #cgo LDFLAGS: -L/opt/homebrew/lib -L/usr/local/lib -L/usr/lib -L/usr/lib/x86_64-linux-gnu -L/usr/lib/aarch64-linux-gnu -lSDL3
// #define SDL_MAIN_HANDLED
// #include <SDL.h>
// #include <SDL_main.h>
//
// static SDL_WindowFlags tiny64WindowFlags(void) {
//     return SDL_WINDOW_RESIZABLE | SDL_WINDOW_HIGH_PIXEL_DENSITY;
// }
import "C"

// The window flags go through a C function rather than being named from
// Go, because SDL3's are 64 bit and spelled SDL_UINT64_C(0x20), which
// expands to "0x20 ## ULL". TinyGo's cgo parses macro bodies itself and
// has no token-pasting operator, so merely naming C.SDL_WINDOW_RESIZABLE
// fails the build - and not on the flag, but pointing at the ## inside
// glibc's <stdint.h>, which takes a while to work out. Expanding them
// inside a function hands that job to clang, which has no such trouble,
// and keeps SDL's names rather than hardcoding the bits they stand for.

// cString lays a Go string out as the NUL-terminated bytes SDL wants.
//
// Every string SDL is handed here - the window title, the hint below - is
// one it copies, so the storage only has to outlive the call, and Go
// memory does that. The alternative, C.CString, allocates with malloc and
// so needs <stdlib.h> in the preamble for the matching free; TinyGo's cgo
// cannot parse that header, because glibc's <stdint.h> underneath it
// defines UINT64_C and friends with the ## token-pasting operator. Not
// including it is easier than working around it.
func cString(s string) []byte {
	return append([]byte(s), 0)
}

// sdlEventSize is sizeof(SDL_Event). Events are polled into a buffer of
// that size rather than a C.SDL_Event so the union is never given a Go
// type: unions are the corner of cgo where TinyGo's reimplementation is
// least like the real one, and the only field this needs is the leading
// Uint32 type tag.
//
// TinyGo's cgo has no C.sizeof_SDL_Event, so the 128 SDL3 pads the union
// to is written out and checked against the header instead: the two arrays
// below have length zero when the constant is right and a length that
// underflows when it is not, so a short buffer that SDL_PollEvent could
// write past the end of fails the build rather than the run.
const sdlEventSize = 128

var (
	_ [sdlEventSize - unsafe.Sizeof(C.SDL_Event{})]byte
	_ [unsafe.Sizeof(C.SDL_Event{}) - sdlEventSize]byte
)

var sdl struct {
	window   *C.SDL_Window
	renderer *C.SDL_Renderer
	frame    *C.SDL_Texture
	palette  *C.SDL_Palette
}

// sdlError wraps whatever SDL last complained about. SDL_GetError returns
// a pointer into SDL's own storage that the next call may overwrite, so
// the string is copied out here rather than held.
func sdlError(what string) error {
	return fmt.Errorf("%s: %s", what, C.GoString(C.SDL_GetError()))
}

// openingScale is how many window pixels the picture gets per emulated dot
// when the window first appears.
//
// Scale alone is not enough. It is a count of pixels, and how big that is
// depends entirely on the panel: 2x is a reasonable window on a 1080p
// desktop and a postage stamp on the 2736x1824 the same code opens on
// next. So Scale is the floor, and the opening size is the largest whole
// multiple of the picture that still leaves room around it on the display
// the window is about to appear on.
//
// Whole multiples only, because a fractional one is what produces uneven
// scanlines - some emulated dots two pixels tall, their neighbours three.
// SDL will happily scale to any size once the user drags the window; this
// is only about not handing them a bad one to begin with.
func openingScale() int {
	// Usable bounds rather than the full display: it excludes the panels
	// and docks the window cannot occupy anyway, which on a desktop with
	// a top bar is the difference between fitting and not.
	var usable C.SDL_Rect
	if !C.SDL_GetDisplayUsableBounds(C.SDL_GetPrimaryDisplay(), &usable) {
		// No display to measure, so there is nothing to derive a size
		// from. The floor is as good a guess as any.
		return Scale
	}

	// Four fifths, so the window opens as a window - large, but with the
	// desktop still visible around it and the title bar still reachable.
	fit := min(
		int(usable.w)*4/5/ScreenWidth,
		int(usable.h)*4/5/ScreenHeight,
	)
	return max(fit, Scale)
}

// makePalette hands SDL the VIC-II's sixteen colours, which is what the
// GPU expands the texture's indices through. SDL takes its own reference
// when the texture is told about it, so this one is still ours to destroy.
func makePalette() (*C.SDL_Palette, error) {
	palette := C.SDL_CreatePalette(C.int(len(tiny64.C64Palette)))
	if palette == nil {
		return nil, sdlError("SDL_CreatePalette")
	}

	colors := make([]C.SDL_Color, len(tiny64.C64Palette))
	for i, c := range tiny64.C64Palette {
		colors[i] = C.SDL_Color{r: C.Uint8(c[0]), g: C.Uint8(c[1]), b: C.Uint8(c[2]), a: C.Uint8(c[3])}
	}
	if !C.SDL_SetPaletteColors(palette, &colors[0], 0, C.int(len(colors))) {
		C.SDL_DestroyPalette(palette)
		return nil, sdlError("SDL_SetPaletteColors")
	}
	return palette, nil
}

// openDisplay brings up the window, the renderer and the paletted texture
// the frame is uploaded into.
func openDisplay(title string) (func(), error) {
	// SDL wants its window and event calls on the thread that set the
	// video mode, and on macOS that must be the main thread. Run is
	// called from main, so pinning here is enough to keep the loop there.
	runtime.LockOSThread()

	C.SDL_SetMainReady()

	// SDL3 installs handlers for SIGINT and SIGTERM but, unlike SDL2,
	// does not turn them into a quit event. The result is an emulator
	// that ignores ctrl-C completely: the signal is caught, nothing is
	// posted, and the loop runs on. Tell SDL to leave signals alone and
	// handle them here instead, so the window and a terminal both reach
	// the same clean shutdown.
	hint, on := cString(C.SDL_HINT_NO_SIGNAL_HANDLERS), cString("1")
	C.SDL_SetHint((*C.char)(unsafe.Pointer(&hint[0])), (*C.char)(unsafe.Pointer(&on[0])))
	notifyQuitSignals()

	if !C.SDL_Init(C.SDL_INIT_VIDEO) {
		return nil, sdlError("SDL_Init")
	}

	closeDisplay := func() {
		// Each of these tolerates nil, so one teardown path serves
		// however far the setup below got.
		C.SDL_DestroyTexture(sdl.frame)
		C.SDL_DestroyPalette(sdl.palette)
		C.SDL_DestroyRenderer(sdl.renderer)
		C.SDL_DestroyWindow(sdl.window)
		C.SDL_Quit()
	}

	cTitle := cString(title)

	// SDL3 centres a new window itself, so unlike SDL2 there is no
	// position to ask for, and a window is shown unless told otherwise.
	scale := openingScale()
	sdl.window = C.SDL_CreateWindow((*C.char)(unsafe.Pointer(&cTitle[0])),
		C.int(ScreenWidth*scale), C.int(ScreenHeight*scale),
		C.tiny64WindowFlags())
	if sdl.window == nil {
		closeDisplay()
		return nil, sdlError("SDL_CreateWindow")
	}
	// Don't let the window shrink below the native picture, where the
	// scaling would start throwing away scanlines.
	C.SDL_SetWindowMinimumSize(sdl.window, ScreenWidth, ScreenHeight)

	sdl.renderer = C.SDL_CreateRenderer(sdl.window, nil)
	if sdl.renderer == nil {
		closeDisplay()
		return nil, sdlError("SDL_CreateRenderer")
	}
	// Vsync is off: pacer, not the panel, decides when a frame ends, and
	// a present that blocks until the next refresh would put that
	// decision back in the display's hands and spend the wait inside SDL
	// rather than in a sleep. The cost is that a frame can be torn, since
	// 50.125Hz divides into no common refresh rate and the swap lands
	// mid-scanout. SDL3 makes this a call rather than a creation flag, so
	// it can be turned back on at runtime if that trade stops being worth
	// it.
	if !C.SDL_SetRenderVSync(sdl.renderer, 0) {
		closeDisplay()
		return nil, sdlError("SDL_SetRenderVSync")
	}

	// A logical size is the whole of the window policy: SDL scales the
	// picture to fill whatever the window currently is, keeps its
	// proportions and letterboxes the remainder.
	//
	// There is deliberately no aspect snap to go with it. Squaring the
	// window up with the picture would mean watching for the resize to
	// stop and then setting the size ourselves, which is fighting the
	// window manager for control of something the user just set. This
	// leaves the window where they put it and lives with the letterbox.
	if !C.SDL_SetRenderLogicalPresentation(sdl.renderer,
		ScreenWidth, ScreenHeight, C.SDL_LOGICAL_PRESENTATION_LETTERBOX) {
		closeDisplay()
		return nil, sdlError("SDL_SetRenderLogicalPresentation")
	}

	palette, err := makePalette()
	if err != nil {
		closeDisplay()
		return nil, err
	}
	sdl.palette = palette

	// INDEX8 is the whole reason for SDL3 here: one byte per pixel,
	// holding the index the VIC-II decided on, expanded to colour by the
	// GPU. Streaming access because the whole texture is rewritten every
	// frame, which is the access pattern locking is for.
	sdl.frame = C.SDL_CreateTexture(sdl.renderer,
		C.SDL_PIXELFORMAT_INDEX8, C.SDL_TEXTUREACCESS_STREAMING,
		ScreenWidth, ScreenHeight)
	if sdl.frame == nil {
		closeDisplay()
		return nil, sdlError("SDL_CreateTexture")
	}
	if !C.SDL_SetTexturePalette(sdl.frame, sdl.palette) {
		closeDisplay()
		return nil, sdlError("SDL_SetTexturePalette")
	}
	// SDL3 filters linearly by default, which on a picture this size
	// means a blurred one. PIXELART is nearest sampling with the edges
	// cleaned up at the non-integer scales a dragged window produces.
	if !C.SDL_SetTextureScaleMode(sdl.frame, C.SDL_SCALEMODE_PIXELART) {
		closeDisplay()
		return nil, sdlError("SDL_SetTextureScaleMode")
	}
	// The picture is opaque and covers the target completely, so there is
	// nothing to blend with.
	C.SDL_SetTextureBlendMode(sdl.frame, C.SDL_BLENDMODE_NONE)

	return closeDisplay, nil
}

// runLoop steps the machine and presents a frame until the window closes.
// A pacer sets the cadence, not the display: see its comment for why the
// panel's refresh rate is the wrong clock for a PAL machine.
func runLoop() error {
	var clock pacer
	for {
		if pumpEvents() {
			return nil
		}

		step()

		if err := present(); err != nil {
			return err
		}

		time.Sleep(clock.advance(time.Now()))
	}
}

// pumpEvents drains SDL's event queue, reporting whether the user asked to
// quit. Key state is not read from here: pollKeyboard samples the keyboard
// as a whole once per frame, but SDL only updates that array while events
// are being pumped, so this has to run regardless.
func pumpEvents() bool {
	if quitSignalled() {
		return true
	}

	var event [sdlEventSize]byte
	for C.SDL_PollEvent((*C.SDL_Event)(unsafe.Pointer(&event[0]))) {
		// The type tag is the first Uint32 of every event in the union.
		switch *(*uint32)(unsafe.Pointer(&event[0])) {
		case C.SDL_EVENT_QUIT:
			return true
		case C.SDL_EVENT_WINDOW_CLOSE_REQUESTED:
			// There is only ever the one window, so the request to
			// close it is the request to stop. SDL raises QUIT of its
			// own accord when the last window goes, but only once the
			// window is actually destroyed; answering the request
			// directly means not depending on that.
			return true
		}
	}
	return false
}

// present uploads the frame the VIC-II just finished and puts it on screen.
func present() error {
	// The texture is STREAMING, so SDL hands over its own staging buffer
	// rather than take a copy of ours. What goes into it is the indexed
	// frame exactly as it stands - one byte per pixel, no expansion -
	// because the texture is INDEX8 and the palette does the rest.
	//
	// SDL owns this pointer until SDL_UnlockTexture, and it is not Go
	// memory, so nothing here can be moved by the collector. The slice is
	// built over it only for the length of the copy.
	var raw unsafe.Pointer
	var pitch C.int
	if !C.SDL_LockTexture(sdl.frame, nil, &raw, &pitch) {
		return sdlError("SDL_LockTexture")
	}
	dst := unsafe.Slice((*byte)(raw), int(pitch)*ScreenHeight)
	src := tiny64.FrameBufferIndexed()
	if int(pitch) == tiny64.FrameBufferStride {
		// The usual case: SDL's rows are as long as ours, so the whole
		// frame goes over as one copy.
		copy(dst, src)
	} else {
		// SDL padded its rows, so they have to go one at a time.
		for y := range ScreenHeight {
			copy(dst[y*int(pitch):], src[y*tiny64.FrameBufferStride:(y+1)*tiny64.FrameBufferStride])
		}
	}
	C.SDL_UnlockTexture(sdl.frame)

	// The letterbox around the picture is SDL's to paint, and it is the
	// only thing the clear is for.
	if !C.SDL_RenderClear(sdl.renderer) {
		return sdlError("SDL_RenderClear")
	}
	if !C.SDL_RenderTexture(sdl.renderer, sdl.frame, nil, nil) {
		return sdlError("SDL_RenderTexture")
	}
	C.SDL_RenderPresent(sdl.renderer)
	return nil
}

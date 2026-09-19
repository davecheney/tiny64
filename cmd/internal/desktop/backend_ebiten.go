//go:build !sdl

package desktop

import "github.com/hajimehoshi/ebiten/v2"

// resizeSettleFrames is how long the window size must sit still before the
// aspect ratio is corrected. Snapping while the user is still dragging means
// fighting the window manager for control of the size, which looks awful, so
// wait for the drag to finish the way VICE does.
const resizeSettleFrames = 12 // ~200ms

// emu is the Ebitengine game. Ebitengine owns the loop and calls back into
// Update and Draw, so unlike the SDL backend there is nowhere to keep this
// on the stack.
var emu emulator

type emulator struct {
	// display hands the GPU palette indices to expand into colours.
	display

	// winW, winH are the window's dimensions as of the previous frame;
	// dragW, dragH are what they were before the current drag started,
	// and settled counts the frames since the size last changed.
	winW, winH   int
	dragW, dragH int
	settled      int
}

// snapWindowToPicture waits for a resize to finish, then squares the window
// up with the picture's proportions so it fills the window exactly and the
// letterbox disappears. Ebitengine exposes neither GLFW's aspect ratio hint
// nor a resize-finished event, hence the settle timer.
func (e *emulator) snapWindowToPicture() {
	// A fullscreen or maximized window belongs to the window manager, not
	// to us; Ebitengine letterboxes those instead. A minimized one
	// reports a size we have no business acting on.
	if ebiten.IsFullscreen() || ebiten.IsWindowMaximized() || ebiten.IsWindowMinimized() {
		return
	}

	w, h := ebiten.WindowSize()
	if w != e.winW || h != e.winH {
		if e.settled > resizeSettleFrames {
			// The window was at rest, so this is the start of a
			// new drag: remember the size it is moving away from.
			e.dragW, e.dragH = e.winW, e.winH
		}
		e.winW, e.winH = w, h
		e.settled = 0
		return
	}
	if e.settled > resizeSettleFrames {
		return // this resize has already been dealt with
	}
	if e.settled++; e.settled < resizeSettleFrames {
		return // still moving, or only just stopped
	}

	// Whichever edge the drag moved furthest is the one the user meant to
	// set, so derive the other from it rather than undoing their work.
	dw, dh := w-e.dragW, h-e.dragH
	if max(dw, -dw) >= max(dh, -dh) {
		h = (w*ScreenHeight + ScreenWidth/2) / ScreenWidth
	} else {
		w = (h*ScreenWidth + ScreenHeight/2) / ScreenHeight
	}
	e.settled = resizeSettleFrames + 1
	if w == e.winW && h == e.winH {
		return // already square with the picture
	}
	ebiten.SetWindowSize(w, h)

	// Read back rather than assume: a window manager may hand back a size
	// other than the one asked for, and remembering what we actually got
	// stops us asking again every frame, forever.
	e.winW, e.winH = ebiten.WindowSize()
}

// Update is called once per frame.
func (e *emulator) Update() error {
	e.snapWindowToPicture()
	step()
	return nil
}

// Draw puts the frame the VIC-II just finished onto the screen.
func (e *emulator) Draw(screen *ebiten.Image) {
	e.display.blit(screen)
}

func (e *emulator) Layout(outsideWidth, outsideHeight int) (int, int) {
	// Tells Ebitengine the logical native canvas size, whatever size the
	// window happens to be. It scales the picture up to fit, keeping its
	// proportions and centring what is left over.
	return ScreenWidth, ScreenHeight
}

// openDisplay compiles the palette shader, allocates the frame texture and
// configures the window. Ebitengine releases all of it when RunGame
// returns, so there is nothing for the caller to close.
func openDisplay(title string) (func(), error) {
	emu = emulator{
		winW:    ScreenWidth * Scale,
		winH:    ScreenHeight * Scale,
		dragW:   ScreenWidth * Scale,
		dragH:   ScreenHeight * Scale,
		settled: resizeSettleFrames + 1, // the opening size needs no correction
	}

	if err := emu.display.init(); err != nil {
		return nil, err
	}

	ebiten.SetWindowSize(emu.winW, emu.winH)
	ebiten.SetWindowTitle(title)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	// Don't let the window shrink below the native picture, where the
	// scaling would start throwing away scanlines.
	ebiten.SetWindowSizeLimits(ScreenWidth, ScreenHeight, -1, -1)

	return func() {}, nil
}

// runLoop hands control to Ebitengine, which drives Update and Draw until
// the window closes.
func runLoop() error {
	return ebiten.RunGame(&emu)
}

// Package desktop provides the shared GUI frontend used by tiny64's
// desktop commands (cmd/c64).
//
// It draws through SDL2, over a small cgo shim. SDL is the backend rather
// than one of several because it is what both compilers can build: this
// used to default to Ebitengine, which reaches TinyGo through purego,
// whose func.go needs reflect.Value.SetPointer, and TinyGo's reflect does
// not have it. With one backend there is no tag to pass and no second
// keyboard map to keep in step.
//
// The package is deliberately isolated from the core tiny64 package: the
// long-term goal is to run tiny64 on a Raspberry Pi Pico 2 under TinyGo,
// which won't use this frontend at all, so nothing here should be
// depended on by anything outside cmd/*.
//
// The drawing lives in backend.go, which supplies two verbs, openDisplay
// and runLoop, and calls step once per frame. Everything else - the
// power-on noise, the reset sequence, the frame count - lives here, where
// the commands that share this frontend can rely on it being the same.
package desktop

import (
	"fmt"
	"math/rand/v2"

	"github.com/davecheney/tiny64"
)

const (
	// The picture the VIC-II actually emits: the blanking intervals are
	// never written, so there's no point sizing the window for them.
	ScreenWidth  = tiny64.VisibleDotsPerLine
	ScreenHeight = tiny64.VisibleLines

	// Scale is the smallest the window will open at, in window pixels per
	// emulated dot. One is too small to read the 40-column screen on
	// anything modern, so two is the floor; openingScale in backend.go
	// picks the actual opening size, which on a large or dense display is
	// a good deal more than this.
	Scale = 2
)

// frames counts the PAL frames the emulator has run, reported on exit as
// a rough check that the machine was actually executing.
var frames int

// step advances the machine by one frame. runLoop calls it once per
// iteration, so the emulated cadence is set here rather than by whatever
// the drawing side happens to be doing.
func step() {
	// Sample the host keyboard once per frame. The matrix itself is
	// combinational, so the guest sees whatever is held at the instant it
	// scans; this only bounds how often that state can change. At 50Hz
	// that is already finer than the KERNAL's own scan interval.
	pollKeyboard(tiny64.Keys())

	tiny64.StepFrame()
	frames++
}

// Run wires the VIC-II's pixel output to a window, randomizes RAM to
// simulate power-on noise, calls setup (if non-nil) so the caller can plug
// in a cartridge or a disk drive before reset, resets the machine, calls
// afterReset (if non-nil) so the caller can drive the freshly booted
// machine before the window takes over, and blocks running the frame loop
// until the window is closed.
//
// afterReset is where tiny64.Autostart goes. It runs the machine itself,
// so the frames it steps are not presented; by the time the loop below
// paints anything the prompt is up and the command has been typed.
func Run(title string, setup, afterReset func()) error {
	defer func() {
		fmt.Println("emulated frames:", frames)
	}()

	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}
	colorRAM := tiny64.ColorRam()
	for i := range colorRAM {
		colorRAM[i] = byte(rand.Uint() & 0x0F)
	}

	closeDisplay, err := openDisplay(title)
	if err != nil {
		return err
	}
	defer closeDisplay()

	if setup != nil {
		setup()
	}

	tiny64.Reset()

	if afterReset != nil {
		afterReset()
	}

	return runLoop()
}

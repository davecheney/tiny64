// Package desktop provides the shared GUI frontend used by tiny64's
// desktop commands (cmd/c64, cmd/destestmax, cmd/deadtest).
//
// It has two backends, chosen by build tag. The default is Ebitengine,
// which expands palette indices into colours on the GPU. Under -tags sdl
// it is SDL2 through a small cgo shim, which is what TinyGo can compile:
// Ebitengine reaches TinyGo through purego, whose func.go needs
// reflect.Value.SetPointer, and TinyGo's reflect does not have it.
//
// The package is deliberately isolated from the core tiny64 package: the
// long-term goal is to run tiny64 on a Raspberry Pi Pico 2 under TinyGo,
// which won't use either backend, so nothing here should be depended on
// by anything outside cmd/*.
//
// A backend supplies two verbs, openDisplay and runLoop, and calls step
// once per frame. Everything else - the power-on noise, the reset
// sequence, the frame count - lives here, so the two backends cannot
// drift on the parts that are not about drawing.
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
	Scale        = 2
)

// frames counts the PAL frames the emulator has run, reported on exit as
// a rough check that the machine was actually executing.
var frames int

// step advances the machine by one frame. Both backends call it once per
// iteration of their loop, so the emulated cadence does not depend on
// which one is built.
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
// in a cartridge or a disk drive before reset, resets the machine, and
// blocks running the frame loop until the window is closed.
func Run(title string, setup func()) error {
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

	return runLoop()
}

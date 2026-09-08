// Package desktop provides the shared Ebitengine-based GUI frontend used
// by tiny64's desktop commands (cmd/c64, cmd/destestmax, cmd/deadtest).
// It is deliberately isolated from the core tiny64 package: the long-term
// goal is to run tiny64 on a Raspberry Pi Pico 2 under TinyGo, which won't
// use Ebitengine at all, so nothing in this package should be depended on
// by anything outside cmd/*.
package desktop

import (
	"fmt"
	"math/rand/v2"

	"github.com/davecheney/tiny64"
	"github.com/hajimehoshi/ebiten/v2"
)

const (
	// The picture the VIC-II actually emits: the blanking intervals are
	// never written, so there's no point sizing the window for them.
	ScreenWidth  = tiny64.VisibleDotsPerLine
	ScreenHeight = tiny64.VisibleLines
	Scale        = 2
)

type emulator struct {
	frames int
}

func newEmulator() *emulator {
	return &emulator{}
}

// Update is called once per frame.
func (e *emulator) Update() error {
	// Sample the host keyboard once per frame. The matrix itself is
	// combinational, so the guest sees whatever is held at the instant it
	// scans; this only bounds how often that state can change. At 50Hz
	// that is already finer than the KERNAL's own scan interval.
	pollKeyboard(tiny64.Keys())

	tiny64.StepFrame()
	e.frames++

	return nil
}

// Draw blits the calculated frame buffer array straight onto the GPU texture.
func (e *emulator) Draw(screen *ebiten.Image) {
	// Blit the raw CPU bytes directly onto the Ebitengine screen texture.
	// This uses highly optimized native OS calls under the hood (Metal on macOS).
	screen.WritePixels(tiny64.FrameBufferRGBA())
}

func (e *emulator) Layout(outsideWidth, outsideHeight int) (int, int) {
	// Tells Ebitengine the logical native canvas size.
	// It handles integer scaling to fit the window seamlessly.
	return ScreenWidth, ScreenHeight
}

// Run wires the VIC-II's pixel output to an Ebitengine window, randomizes
// RAM to simulate power-on noise, calls insertCart (if non-nil) so the
// caller can plug in a cartridge before reset, resets the machine, and
// blocks running the game loop until the window is closed.
func Run(title string, insertCart func()) error {
	emu := newEmulator()
	defer func() {
		fmt.Println("emulated frames:", emu.frames)
	}()

	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}

	if insertCart != nil {
		insertCart()
	}

	tiny64.Reset()

	ebiten.SetWindowSize(ScreenWidth*Scale, ScreenHeight*Scale)
	ebiten.SetWindowTitle(title)

	return ebiten.RunGame(emu)
}

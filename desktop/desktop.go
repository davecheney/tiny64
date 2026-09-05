// Package desktop provides the shared Ebitengine-based GUI frontend used
// by tiny64's desktop commands (cmd/tiny64, cmd/destestmax, cmd/deadtest).
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
	// Full PAL VIC-II screen dimension including borders
	ScreenWidth  = tiny64.DotsPerLine
	ScreenHeight = tiny64.RasterLinesPerFrame
	Scale        = 2
)

// C64Palette is the C64 PAL color palette (RGBA format).
var C64Palette = [16][4]byte{
	{0x00, 0x00, 0x00, 0xff}, // 0: Black
	{0xff, 0xff, 0xff, 0xff}, // 1: White
	{0x88, 0x00, 0x00, 0xff}, // 2: Red
	{0xaa, 0xff, 0xee, 0xff}, // 3: Cyan
	{0xcc, 0x44, 0xcc, 0xff}, // 4: Purple
	{0x00, 0xcc, 0x55, 0xff}, // 5: Green
	{0x00, 0x00, 0xaa, 0xff}, // 6: Blue
	{0xee, 0xee, 0x77, 0xff}, // 7: Yellow
	{0xdd, 0x88, 0x55, 0xff}, // 8: Orange
	{0x66, 0x44, 0x00, 0xff}, // 9: Brown
	{0xff, 0x77, 0x77, 0xff}, // 10: Light Red
	{0x33, 0x33, 0x33, 0xff}, // 11: Dark Gray
	{0x77, 0x77, 0x77, 0xff}, // 12: Medium Gray
	{0xaa, 0xff, 0x66, 0xff}, // 13: Light Green
	{0x00, 0x88, 0xff, 0xff}, // 14: Light Blue
	{0xbb, 0xbb, 0xbb, 0xff}, // 15: Light Gray
}

type emulator struct {
	// Raw linear pixel data (4 bytes per pixel: R, G, B, A)
	frameBuffer []byte

	frames int
}

func newEmulator() *emulator {
	return &emulator{
		frameBuffer: make([]byte, ScreenWidth*ScreenHeight*4),
	}
}

// Update is called once per frame.
func (e *emulator) Update() error {
	const dots = tiny64.DotsPerLine * tiny64.RasterLinesPerFrame // 63 cycles * 8 dots * 312 raster lines
	for range dots {
		tiny64.VIC().StepDot()
	}
	e.frames++

	return nil
}

// writePixelToBuffer maps a C64 color index (0-15) directly into the flat RGBA slice.
func (e *emulator) writePixelToBuffer(x, y int, colorIndex byte) {
	if x < 0 || x >= ScreenWidth || y < 0 || y >= ScreenHeight {
		return
	}
	idx := (y*ScreenWidth + x) * 4
	rgba := C64Palette[colorIndex&0xF]

	e.frameBuffer[idx] = rgba[0]   // R
	e.frameBuffer[idx+1] = rgba[1] // G
	e.frameBuffer[idx+2] = rgba[2] // B
	e.frameBuffer[idx+3] = rgba[3] // A
}

// Draw blits the calculated frame buffer array straight onto the GPU texture.
func (e *emulator) Draw(screen *ebiten.Image) {
	// Blit the raw CPU bytes directly onto the Ebitengine screen texture.
	// This uses highly optimized native OS calls under the hood (Metal on macOS).
	screen.WritePixels(e.frameBuffer)
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

	tiny64.VIC().WritePixelToBuffer = emu.writePixelToBuffer

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

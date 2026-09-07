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

	// The buffer is full raster height, not ScreenHeight: that costs a few
	// unused rows but means a pixel write needs neither an offset nor a
	// bounds check, since the VIC only emits x < ScreenWidth and y is
	// always a valid raster line. Draw slices the vblank rows back off.
	bufferHeight = tiny64.RasterLinesPerFrame
	vblankBytes  = tiny64.FirstVisibleLine * ScreenWidth * 4
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
		frameBuffer: make([]byte, ScreenWidth*bufferHeight*4),
	}
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

// writePixelToBuffer maps a C64 color index (0-15) directly into the flat RGBA slice.
func (e *emulator) writePixelToBuffer(x, y uint16, colorIndex byte) {
	idx := (int(y)*ScreenWidth + int(x)) * 4
	copy(e.frameBuffer[idx:idx+4], C64Palette[colorIndex&0xF][:])
}

// Draw blits the calculated frame buffer array straight onto the GPU texture.
func (e *emulator) Draw(screen *ebiten.Image) {
	// Blit the raw CPU bytes directly onto the Ebitengine screen texture.
	// This uses highly optimized native OS calls under the hood (Metal on macOS).
	screen.WritePixels(e.frameBuffer[vblankBytes : vblankBytes+ScreenHeight*ScreenWidth*4])
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

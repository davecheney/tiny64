package main

import (
	"fmt"
	"log"

	"github.com/davecheney/tiny64"
	"github.com/hajimehoshi/ebiten/v2"
)

const (
	// Full PAL VIC-II screen dimension including borders
	ScreenWidth  = tiny64.DotsPerLine
	ScreenHeight = tiny64.RasterLinesPerFrame
)

// C64 PAL color palette (RGBA format)
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

type Emulator struct {
	// Raw linear pixel data (4 bytes per pixel: R, G, B, A)
	frameBuffer []byte

	frames int
}

func NewEmulator() *Emulator {
	return &Emulator{
		frameBuffer: make([]byte, ScreenWidth*ScreenHeight*4),
	}
}

// Update is called once per frame.
func (e *Emulator) Update() error {
	const dots = tiny64.DotsPerLine * tiny64.RasterLinesPerFrame // 63 cycles * 8 dots * 312 raster lines
	for range dots {
		tiny64.VIC().StepDot()
	}
	e.frames++

	return nil
}

// WritePixelToBuffer maps a C64 color index (0-15) directly into your flat RGBA slice
func (e *Emulator) writePixelToBuffer(x, y int, colorIndex byte) {
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

// Draw blits your calculated frame buffer array straight onto the GPU texture
func (e *Emulator) Draw(screen *ebiten.Image) {
	// Blit the raw CPU bytes directly onto the Ebitengine screen texture.
	// This uses highly optimized native OS calls under the hood (Metal on macOS).
	screen.WritePixels(e.frameBuffer)
}

func (e *Emulator) Layout(outsideWidth, outsideHeight int) (int, int) {
	// Tells Ebitengine the logical native canvas size.
	// It handles integer scaling to fit the window seamlessly.
	return ScreenWidth, ScreenHeight
}

func main() {
	emu := NewEmulator()

	defer func() {
		fmt.Println("emulated frames:", emu.frames)
	}()

	tiny64.VIC().WritePixelToBuffer = emu.writePixelToBuffer

	// // fill ram with random values to simulate power on randomness
	// ram := tiny64.Ram()
	// for i := range ram {
	// 	ram[i] = byte(rand.Uint())
	// }

	tiny64.DisableCIAInterrupts = true
	tiny64.Reset()

	// Diagnostic: bypass KERNAL entirely and drive the VIC-II directly, to
	// check for pixel/column alignment bugs in isolation. Every one of the
	// 25 screen rows is filled with the same 40-character sequence
	// starting at PETSCII/screen-code 'A' (1), so any horizontal shift is
	// immediately visible as an offset of the "ABCDEFGH..." pattern from
	// the left edge of the display window.
	vic := tiny64.VIC()
	vic.WriteRegister(0xD020, 0x0E) // border: light blue
	vic.WriteRegister(0xD021, 0x06) // background: blue
	vic.WriteRegister(0xD011, 0x1B) // DEN=1, RSEL=1 (25 rows), YSCROLL=3
	vic.WriteRegister(0xD016, 0x08) // CSEL=1 (40 cols), MCM=0, XSCROLL=0
	vic.WriteRegister(0xD018, 0x14) // VM=1 (screen @ $0400), CB=2 (chars @ $1000)

	ram := tiny64.Ram()
	for row := range 25 {
		for col := range 40 {
			ram[0x0400+row*40+col] = byte(1 + col) // screen code 1 = 'A'
			ram[0xD800+row*40+col] = 0x01          // white foreground
		}
	}

	// A tiny loop program at $0200 (JMP $0200) so the CPU just spins
	// instead of running BASIC/KERNAL and disturbing the registers/memory
	// set up above.
	ram[0x0200] = 0x4C // JMP
	ram[0x0201] = 0x00
	ram[0x0202] = 0x02
	tiny64.GetCPU().PC = 0x0200

	ebiten.SetWindowSize(ScreenWidth*2, ScreenHeight*2) // Open window scaled 2x
	ebiten.SetWindowTitle("Go-64 Emulator Surface")

	if err := ebiten.RunGame(emu); err != nil {
		log.Fatal(err)
	}
}

//go:build tinygo

// Command tiny64 runs the emulator on a TinyGo board with an ST7789
// panel. It imports machine, so it only builds under TinyGo.
package main

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"time"

	"machine"

	"github.com/davecheney/tiny64"
	"tinygo.org/x/drivers/pixel"
	"tinygo.org/x/drivers/st7789"
)

// C64Palette is the C64 PAL color palette (RGBA format).
var C64Palette = [16]pixel.RGB565BE{
	pixel.NewRGB565BE(0x00, 0x00, 0x00), // 0: Black
	pixel.NewRGB565BE(0xff, 0xff, 0xff), // 1: White
	pixel.NewRGB565BE(0x88, 0x00, 0x00), // 2: Red
	pixel.NewRGB565BE(0xaa, 0xff, 0xee), // 3: Cyan
	pixel.NewRGB565BE(0xcc, 0x44, 0xcc), // 4: Purple
	pixel.NewRGB565BE(0x00, 0xcc, 0x55), // 5: Green
	pixel.NewRGB565BE(0x00, 0x00, 0xaa), // 6: Blue
	pixel.NewRGB565BE(0xee, 0xee, 0x77), // 7: Yellow
	pixel.NewRGB565BE(0xdd, 0x88, 0x55), // 8: Orange
	pixel.NewRGB565BE(0x66, 0x44, 0x00), // 9: Brown
	pixel.NewRGB565BE(0xff, 0x77, 0x77), // 10: Light Red
	pixel.NewRGB565BE(0x33, 0x33, 0x33), // 11: Dark Gray
	pixel.NewRGB565BE(0x77, 0x77, 0x77), // 12: Medium Gray
	pixel.NewRGB565BE(0xaa, 0xff, 0x66), // 13: Light Green
	pixel.NewRGB565BE(0x00, 0x88, 0xff), // 14: Light Blue
	pixel.NewRGB565BE(0xbb, 0xbb, 0xbb), // 15: Light Gray
}

// The 320x240 panel can't show the whole 405x284 picture, so crop to the
// 40-column display window (dots 48-367, exactly 320 wide) and centre the
// 25-row window (raster 51-250) vertically, leaving 20 lines of border
// above and below.
const (
	cropX = 48
	cropY = 31
)

var fb = pixel.NewImage[pixel.RGB565BE](320, 240)

func main() {
	machine.SPI0.Configure(machine.SPIConfig{
		Frequency: 16_000_000, // was 8000000 - display transfer was now the bottleneck
		Mode:      0,
	})

	display := st7789.New(machine.SPI0,
		machine.TFT_RST,       // TFT_RESET
		machine.TFT_WRX,       // TFT_DC
		machine.TFT_CS,        // TFT_CS
		machine.TFT_BACKLIGHT) // TFT_LITE

	display.Configure(st7789.Config{
		Rotation: st7789.ROTATION_270,
		Height:   320,
	})

	// Clear the screen to black
	display.FillScreen(color.RGBA{0, 0, 0, 255})
	tiny64.WritePixelToBuffer = func(x, y uint16, colorIndex byte) {
		x -= cropX // underflows out of range below the crop origin
		y -= cropY
		if x >= 320 || y >= 240 {
			return
		}
		fb.Set(int(x), int(y), C64Palette[colorIndex])
	}

	// Fill RAM with random values to simulate power-on randomness.
	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}

	tiny64.Reset()

	blink := func() func() {
		var state bool
		led := machine.LED
		led.Configure(machine.PinConfig{Mode: machine.PinOutput})
		return func() {
			state = !state
			led.Set(state)
		}
	}()

	render := make(chan int, 1)
	go func() {
		for range render {
			display.DrawBitmap(0, 0, fb)
		}
	}()

	var emulateTime, drawTime time.Duration
	for frame := 0; ; frame++ {
		blink()
		if frame%50 == 0 && frame > 0 {
			fmt.Printf("frame %d: emulate=%v draw=%v (avg over 50 frames)\n",
				frame, emulateTime/50, drawTime/50)
			emulateTime, drawTime = 0, 0
		}
		start := time.Now()
		tiny64.StepFrame()
		emulateTime += time.Since(start)

		start = time.Now()
		render <- 1
		drawTime += time.Since(start)
	}
}

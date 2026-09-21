//go:build gopher_badge

// Command tiny64 runs the emulator on the Gopher Badge, a TinyGo board
// with an ST7789 panel. It imports machine and the badge's own pins, so it
// is behind the gopher_badge build tag that TinyGo sets for that target.
package main

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"runtime"
	"time"

	"machine"

	"github.com/davecheney/tiny64"
	"tinygo.org/x/drivers/pixel"
	"tinygo.org/x/drivers/st7789"
)

func renderFrames(display *st7789.Device, frames <-chan int) {
	fb := pixel.NewImageFromBytes[pixel.RGB565BE](320, 240, tiny64.FrameBufferRGB565BE())
	for range frames {
		display.DrawBitmap(0, 0, fb)
	}
}

func main() {
	machine.SPI0.Configure(machine.SPIConfig{
		Frequency: 32_000_000, // keep the queued frame transfer below emulation time
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

	// Fill RAM with random values to simulate power-on randomness.
	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}
	colorRAM := tiny64.ColorRam()
	for i := range colorRAM {
		colorRAM[i] = byte(rand.Uint() & 0x0F)
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
	go renderFrames(&display, render)

	var emulateTime, drawTime time.Duration
	var heap runtime.MemStats
	var lastGC uint32
	var lastAlloc, lastMallocs uint64
	for frame := 0; ; frame++ {
		blink()
		if frame%50 == 0 && frame > 0 {
			// Asked here rather than beside StepFrame because it is not a
			// counter read: on the block collector this walks the whole
			// metadata bitmap under the GC's own lock, so sampling it per
			// frame would show up in the frame time it is reporting on.
			runtime.ReadMemStats(&heap)

			// This board renders from a goroutine, so unlike the Tufty it
			// has a scheduler and a second stack in the heap. gc= is
			// still expected to stay at 0: what the render goroutine is
			// handed is the frame buffer the emulator already owns.
			fmt.Printf("frame %d: emulate=%v draw=%v gc=%d alloc=%dB mallocs=%d heap=%d/%d (avg over 50 frames)\n",
				frame, emulateTime/50, drawTime/50,
				heap.NumGC-lastGC, (heap.TotalAlloc-lastAlloc)/50,
				heap.Mallocs-lastMallocs, heap.HeapInuse, heap.HeapSys)

			lastGC, lastAlloc, lastMallocs = heap.NumGC, heap.TotalAlloc, heap.Mallocs
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

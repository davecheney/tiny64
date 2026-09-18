//go:build tinygo

// Command tiny64 runs the emulator on a TinyGo board with an ST7789
// panel. It imports machine, so it only builds under TinyGo.
package main

import (
	"fmt"
	"image/color"
	"math/rand/v2"
	"time"

	"device/rp"
	"machine"

	"github.com/davecheney/tiny64"
	"tinygo.org/x/drivers/pixel"
	"tinygo.org/x/drivers/st7789"
)

// xipCacheDelta reads the RP2040's XIP cache hit/access counters
// (XIP_CTRL.CTR_HIT/CTR_ACC), two free-running 32-bit counters that
// increment on every flash (XIP) access the cache serves and every access
// it sees at all, respectively. They are never reset by hardware, so the
// caller keeps the previous reading and this returns hit/access deltas
// since then - a window's cache hit rate, which tracks pressure from one
// frame-batch to the next rather than a lifetime average that would just
// flatten towards "mostly hits" over a long run. uint32 subtraction
// handles the counters wrapping between reads; nothing here runs
// anywhere near the ~4 billion accesses that would need to happen within
// one window to wrap twice.
func xipCacheDelta(prevHit, prevAcc uint32) (hit, acc, curHit, curAcc uint32) {
	curHit = rp.XIP_CTRL.CTR_HIT.Get()
	curAcc = rp.XIP_CTRL.CTR_ACC.Get()
	return curHit - prevHit, curAcc - prevAcc, curHit, curAcc
}

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
	var prevXIPHit, prevXIPAcc uint32
	for frame := 0; ; frame++ {
		blink()
		if frame%50 == 0 && frame > 0 {
			hit, acc, curHit, curAcc := xipCacheDelta(prevXIPHit, prevXIPAcc)
			prevXIPHit, prevXIPAcc = curHit, curAcc
			var hitPct float64
			if acc > 0 {
				hitPct = 100 * float64(hit) / float64(acc)
			}
			fmt.Printf("frame %d: emulate=%v draw=%v (avg over 50 frames) xip=%.2f%% hits=%d accesses=%d misses=%d\n",
				frame, emulateTime/50, drawTime/50, hitPct, hit, acc, acc-hit)
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

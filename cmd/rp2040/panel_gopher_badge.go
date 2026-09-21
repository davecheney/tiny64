//go:build gopher_badge

package main

import (
	"image/color"

	"machine"

	"github.com/davecheney/tiny64"
	"tinygo.org/x/drivers/pixel"
	"tinygo.org/x/drivers/st7789"
)

// spiBaud is the panel's clock, and on this board it is the frame rate.
// The transfer is bus-bound - doubling the clock halved it to the last
// digit - so this is set as high as the RP2040's SPI goes, which is half
// the peripheral clock.
const spiBaud = 62_500_000

// spiST7789 is the Gopher Badge's panel: the same ST7789 the Tufty has,
// wired over SPI instead of an eight bit parallel bus, driven through the
// stock driver rather than a hand-rolled one.
//
// The Tufty's transfer is asynchronous in hardware - PIO feeds DMA and
// the CPU walks away - and this one is not: DrawBitmap blocks until the
// frame is out. The goroutine below is meant to buy that overlap back,
// and under -scheduler=cores it would, on the second core. It does not
// under -scheduler=tasks, which is what this board is built with and why:
// tasks is cooperative on one core, so the draw runs between frames
// rather than beside them and wait carries all of it. See main.go for
// what cores does instead, which is nothing at all on USB serial.
//
// So the goroutine is currently machinery for a scheduler that does not
// work. It stays because it is what makes the overlap available the day
// it does, and because it costs 5us a frame to hand the frame over.
//
// frames carries the request and done carries the acknowledgement, so
// wait blocks on the transfer actually being finished rather than on the
// channel having room. frames is buffered by one so that start hands the
// frame over and returns even if the render goroutine has not been
// scheduled yet; done is unbuffered because wait has nothing to do until
// the frame is out.
type spiST7789 struct {
	frames  chan struct{}
	done    chan struct{}
	pending bool
}

// wait blocks until the frame started last time is on the panel.
func (st *spiST7789) wait() {
	if !st.pending {
		return
	}
	<-st.done
	st.pending = false
}

// start hands the frame to the render goroutine and returns.
//
// The frame is not copied. It is the emulator's own buffer, and the loop
// in main does not touch it again until after the next wait, so the
// goroutine has it to itself for as long as it is reading.
func (st *spiST7789) start(frame []byte) error {
	st.frames <- struct{}{}
	st.pending = true
	return nil
}

// configurePanel brings up SPI0 and the panel, and starts the goroutine
// that does the drawing.
func configurePanel() (*spiST7789, error) {
	machine.SPI0.Configure(machine.SPIConfig{
		Frequency: spiBaud,
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
	display.FillScreen(color.RGBA{0, 0, 0, 255})

	st := &spiST7789{
		frames: make(chan struct{}, 1),
		done:   make(chan struct{}),
	}
	go func() {
		// Built once: the image is a view over the emulator's frame
		// buffer, which does not move, so there is nothing to rebuild
		// per frame and nothing here allocates.
		fb := pixel.NewImageFromBytes[pixel.RGB565BE](
			displayWidth, displayHeight, tiny64.FrameBufferRGB565BE())
		for range st.frames {
			display.DrawBitmap(0, 0, fb)
			st.done <- struct{}{}
		}
	}()
	return st, nil
}

// buttonState tracks the two buttons this board gives the machine, so
// each acts on its press edge rather than for as long as it is held.
//
// The Badge has no C, so unlike the Tufty there is no RESTORE-alone key.
// A and B are the two that matter and they do the same thing on both.
type buttonState struct {
	a, b bool
}

// configureButtons readies the board's buttons.
//
// They are active low here, which is the opposite of the Tufty's. The
// board pulls them up and a press shorts to ground, so configured with a
// pull-down - which is what the Tufty wants - every one of them reads as
// held down for ever. That is not a cosmetic difference: BUTTON_A is a
// cold reset, so the machine reset on every frame, never got far enough
// to load the demo, and sat at a READY. prompt looking like a hang.
func configureButtons() (*buttonState, error) {
	for _, pin := range []machine.Pin{
		machine.BUTTON_A,
		machine.BUTTON_B,
	} {
		pin.Configure(machine.PinConfig{Mode: machine.PinInputPullup})
	}
	return &buttonState{}, nil
}

// pressed reads one of this board's active-low buttons.
func pressed(pin machine.Pin) bool { return !pin.Get() }

func (s *buttonState) poll() {
	a := pressed(machine.BUTTON_A)
	if a && !s.a {
		// Button A edge: cold reset, and start the demo again.
		randomiseMemory()
		tiny64.Keys().ReleaseAll()
		tiny64.Reset()
		tiny64.Autostart()
	}
	s.a = a

	b := pressed(machine.BUTTON_B)
	if b && !s.b {
		// Button B edge: RUN/STOP + RESTORE, the C64's warm reset.
		tiny64.Keys().Press(tiny64.KeyRunStop)
		tiny64.Keys().Restore()
	} else if !b && s.b {
		tiny64.Keys().Release(tiny64.KeyRunStop)
	}
	s.b = b
}

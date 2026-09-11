//go:build tinygo

// Command tufty2040 runs the emulator on the Pimoroni Tufty 2040.
package main

import (
	"fmt"
	"math/rand/v2"
	"time"

	"machine"

	"github.com/davecheney/tiny64"
	pio "github.com/tinygo-org/pio/rp2-pio"
	"github.com/tinygo-org/pio/rp2-pio/piolib"
)

const busBaud = 15_000_000

func configureDisplay() (*parallelST7789, error) {
	machine.LCD_CS.Configure(machine.PinConfig{Mode: machine.PinOutput})
	machine.LCD_CS.High()
	machine.LCD_DC.Configure(machine.PinConfig{Mode: machine.PinOutput})
	machine.LCD_DC.High()
	machine.LCD_RD.Configure(machine.PinConfig{Mode: machine.PinOutput})
	machine.LCD_RD.High()

	sm, err := pio.PIO0.ClaimStateMachine()
	if err != nil {
		return nil, err
	}
	bus, err := piolib.NewParallel(sm, piolib.ParallelConfig{
		Baud:        busBaud,
		Clock:       machine.LCD_WR,
		DataBase:    machine.LCD_DB0,
		BusWidth:    8,
		BitsPerPull: 8,
	})
	if err != nil {
		return nil, err
	}
	if err := bus.EnableDMA(true); err != nil {
		return nil, err
	}

	display := &parallelST7789{
		cs: machine.LCD_CS,
		dc: machine.LCD_DC,
		rd: machine.LCD_RD,
		bl: machine.LCD_BACKLIGHT,
		pl: bus,
	}
	if err := display.init(); err != nil {
		return nil, err
	}
	return display, nil
}

func main() {
	display, err := configureDisplay()
	if err != nil {
		panic(err.Error())
	}

	// Fill RAM with random values to simulate power-on randomness.
	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}
	tiny64.AttachVirtualPRG(8, "MAZE", mazePRG)
	tiny64.Reset()

	var demo demoLoader
	var emulateTime, waitTime, startDrawTime time.Duration
	for frame := 0; ; frame++ {
		if frame%50 == 0 && frame > 0 {
			fmt.Printf("frame %d: emulate=%v wait=%v start=%v (avg over 50 frames)\n",
				frame, emulateTime/50, waitTime/50, startDrawTime/50)
			emulateTime, waitTime, startDrawTime = 0, 0, 0
		}

		start := time.Now()
		tiny64.StepFrame()
		emulateTime += time.Since(start)
		demo.tick()

		start = time.Now()
		display.waitDisplay()
		waitTime += time.Since(start)

		start = time.Now()
		if err := display.startDisplay(tiny64.FrameBufferRGB565BE()); err != nil {
			panic(err.Error())
		}
		startDrawTime += time.Since(start)
	}
}

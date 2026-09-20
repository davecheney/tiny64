//go:build tinygo

// Command tufty2040 runs the emulator on the Pimoroni Tufty 2040.
package main

import (
	"fmt"
	"math/rand/v2"
	"time"

	"device/rp"
	"machine"

	"github.com/davecheney/tiny64"
	pio "github.com/tinygo-org/pio/rp2-pio"
	"github.com/tinygo-org/pio/rp2-pio/piolib"
)

const busBaud = 15_000_000

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

func configureButtons() {
	for _, pin := range []machine.Pin{
		machine.BUTTON_A,
		machine.BUTTON_B,
		machine.BUTTON_C,
		machine.BUTTON_UP,
		machine.BUTTON_DOWN,
	} {
		pin.Configure(machine.PinConfig{Mode: machine.PinInputPulldown})
	}
}

type buttonState struct {
	a, b, c bool
}

func (s *buttonState) poll() {
	a := machine.BUTTON_A.Get()
	if a && !s.a {
		// Button A edge: Cold Reset / Restart entire demo
		ram := tiny64.Ram()
		for i := range ram {
			ram[i] = byte(rand.Uint())
		}
		colorRAM := tiny64.ColorRam()
		for i := range colorRAM {
			colorRAM[i] = byte(rand.Uint() & 0x0F)
		}
		tiny64.Keys().ReleaseAll()
		tiny64.Reset()
		tiny64.Autostart()
	}
	s.a = a

	b := machine.BUTTON_B.Get()
	if b && !s.b {
		// Button B edge: RUN/STOP + RESTORE (C64 Warm Reset)
		tiny64.Keys().Press(tiny64.KeyRunStop)
		tiny64.Keys().Restore()
	} else if !b && s.b {
		tiny64.Keys().Release(tiny64.KeyRunStop)
	}
	s.b = b

	c := machine.BUTTON_C.Get()
	if c && !s.c {
		// Button C edge: RESTORE alone (NMI pulse)
		tiny64.Keys().Restore()
	}
	s.c = c
}

func main() {
	configureButtons()
	display, err := configureDisplay()
	if err != nil {
		panic(err.Error())
	}

	// Fill RAM with random values to simulate power-on randomness.
	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}
	colorRAM := tiny64.ColorRam()
	for i := range colorRAM {
		colorRAM[i] = byte(rand.Uint() & 0x0F)
	}
	tiny64.AttachVirtualPRG(8, "MAZE", mazePRG)
	tiny64.Reset()

	// Autostart types the load itself through the KERNAL's type-ahead
	// buffer, so the 8K wedge EPROM is no longer carried to do it.
	tiny64.Autostart()

	var buttons buttonState
	var emulateTime, waitTime, startDrawTime time.Duration
	var prevXIPHit, prevXIPAcc uint32
	for frame := 0; ; frame++ {
		buttons.poll()
		if frame%50 == 0 && frame > 0 {
			hit, acc, curHit, curAcc := xipCacheDelta(prevXIPHit, prevXIPAcc)
			prevXIPHit, prevXIPAcc = curHit, curAcc
			var hitPct float64
			if acc > 0 {
				hitPct = 100 * float64(hit) / float64(acc)
			}
			fmt.Printf("frame %d: emulate=%v wait=%v start=%v (avg over 50 frames) xip=%.2f%% hits=%d accesses=%d misses=%d maze=%v\n",
				frame, emulateTime/50, waitTime/50, startDrawTime/50, hitPct, hit, acc, acc-hit, mazeRunning())
			emulateTime, waitTime, startDrawTime = 0, 0, 0
		}

		start := time.Now()
		tiny64.StepFrame()
		emulateTime += time.Since(start)

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

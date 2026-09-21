//go:build tufty2040 || gopher_badge

// Command rp2040 runs the emulator on an RP2040 board with an ST7789
// panel. Two are supported and they are the same machine: a Cortex-M0+
// running the C64 from flash, 320x240 of RGB565 in SRAM, and a panel to
// push it at. What differs is how the panel is wired - the Pimoroni Tufty
// 2040 drives it over an eight bit parallel bus through PIO and DMA, the
// Gopher Badge over SPI - and which buttons the board has.
//
// That difference lives in panel_tufty2040.go and panel_gopher_badge.go,
// one of which is compiled, selected by the build tag TinyGo sets from
// -target. Each defines a panel with the same two methods and a
// buttonState with the same one, so what is below never asks which board
// it is on:
//
//	tinygo flash -target=tufty2040    -opt=2 -scheduler=none  ./cmd/rp2040
//	tinygo flash -target=gopher-badge -opt=2 -scheduler=tasks ./cmd/rp2040
//
// The scheduler settings are not interchangeable. The Tufty's panel is
// asynchronous in hardware - DMA runs while the next frame is emulated -
// and wants no scheduler at all. The Badge's is synchronous, so it needs
// one for the goroutine that draws.
//
// -scheduler=cores is what the Badge wants and does not currently work:
// under TinyGo 0.43.0-dev the board comes up and draws but emits nothing
// on USB serial, so none of the telemetry below arrives. tasks is what
// was measured. The difference is real work, not just reporting - tasks
// is cooperative on one core, so the draw does not overlap the next
// frame and wait= carries the whole 58ms transfer - but a board that
// cannot be measured is worse than a board that is slower.
package main

import (
	"fmt"
	"math/rand/v2"
	"runtime"
	"time"

	"device/rp"

	"github.com/davecheney/tiny64"
)

const (
	// The panel both boards carry, and the window of the C64's picture
	// the bare-metal frame buffer holds. pixel_sink_baremetal.go crops to
	// exactly this, so a frame is handed over whole with no scaling.
	displayWidth  = 320
	displayHeight = 240
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
//
// It is worth more than it looks. Frame time on this chip moves by a
// couple of percent between two builds that do identical work, purely on
// where their code lands in flash; the miss count is what tells that
// apart from a change that actually did less.
func xipCacheDelta(prevHit, prevAcc uint32) (hit, acc, curHit, curAcc uint32) {
	curHit = rp.XIP_CTRL.CTR_HIT.Get()
	curAcc = rp.XIP_CTRL.CTR_ACC.Get()
	return curHit - prevHit, curAcc - prevAcc, curHit, curAcc
}

// randomiseMemory fills RAM and colour RAM the way a cold power-on finds
// them. The KERNAL's memory test walks over RAM regardless, but what is
// left in colour RAM shows on screen until something writes it, and a
// zeroed machine hides bugs a real one would not.
func randomiseMemory() {
	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}
	colorRAM := tiny64.ColorRam()
	for i := range colorRAM {
		colorRAM[i] = byte(rand.Uint() & 0x0F)
	}
}

func main() {
	buttons, err := configureButtons()
	if err != nil {
		panic(err.Error())
	}
	display, err := configurePanel()
	if err != nil {
		panic(err.Error())
	}

	randomiseMemory()
	tiny64.AttachVirtualPRG(8, "MAZE", mazePRG)
	tiny64.Reset()

	// Autostart types the load through the KERNAL's own type-ahead
	// buffer, so nothing is patched and no wedge ROM is carried to do it.
	tiny64.Autostart()

	var emulateTime, waitTime, startDrawTime time.Duration
	var prevXIPHit, prevXIPAcc uint32
	var heap runtime.MemStats
	var lastGC uint32
	var lastAlloc, lastMallocs uint64
	for frame := 0; ; frame++ {
		buttons.poll()
		if frame%50 == 0 && frame > 0 {
			hit, acc, curHit, curAcc := xipCacheDelta(prevXIPHit, prevXIPAcc)
			prevXIPHit, prevXIPAcc = curHit, curAcc
			var hitPct float64
			if acc > 0 {
				hitPct = 100 * float64(hit) / float64(acc)
			}

			// Asked here rather than beside StepFrame because it is not a
			// counter read: on the block collector this walks the whole
			// metadata bitmap under the GC's own lock, so sampling it per
			// frame would show up in the frame time it is reporting on.
			runtime.ReadMemStats(&heap)

			// gc= and alloc= answer whether the frame loop allocates at
			// all. It should not, so gc= staying 0 is the expected
			// reading rather than an interesting one; a collection that
			// did land inside a frame would be frame time appearing from
			// nowhere. maze= says the numbers beside it are a
			// measurement of the demo rather than of an idle READY.
			fmt.Printf("frame %d: emulate=%v wait=%v start=%v (avg over 50 frames) xip=%.2f%% hits=%d accesses=%d misses=%d gc=%d alloc=%dB mallocs=%d heap=%d/%d maze=%v\n",
				frame, emulateTime/50, waitTime/50, startDrawTime/50,
				hitPct, hit, acc, acc-hit,
				heap.NumGC-lastGC, (heap.TotalAlloc-lastAlloc)/50,
				heap.Mallocs-lastMallocs, heap.HeapInuse, heap.HeapSys,
				mazeRunning())

			lastGC, lastAlloc, lastMallocs = heap.NumGC, heap.TotalAlloc, heap.Mallocs
			emulateTime, waitTime, startDrawTime = 0, 0, 0
		}

		start := time.Now()
		tiny64.StepFrame()
		emulateTime += time.Since(start)

		// The previous frame is still going out while this one is
		// emulated, on both boards; this is where that overlap is
		// collected. wait= being near zero is what says the panel keeps
		// up with the emulator rather than the other way round.
		start = time.Now()
		display.wait()
		waitTime += time.Since(start)

		start = time.Now()
		if err := display.start(tiny64.FrameBufferRGB565BE()); err != nil {
			panic(err.Error())
		}
		startDrawTime += time.Since(start)
	}
}

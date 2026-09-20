// Package desktop provides the shared GUI frontend used by tiny64's
// desktop commands (cmd/c64).
//
// It draws through SDL3, over a small cgo shim. SDL is the backend rather
// than one of several because it is what both compilers can build: this
// used to default to Ebitengine, which reaches TinyGo through purego,
// whose func.go needs reflect.Value.SetPointer, and TinyGo's reflect does
// not have it. With one backend there is no tag to pass and no second
// keyboard map to keep in step.
//
// SDL3 rather than SDL2 because of SDL_SetTexturePalette: the VIC-II
// decides on a palette index per pixel, and a paletted texture is that
// shape exactly, so a frame goes to the GPU as one byte per pixel and is
// expanded to colour there. Under SDL2 the expansion had to happen on the
// CPU first, at four times the bytes.
//
// The package is deliberately isolated from the core tiny64 package: the
// long-term goal is to run tiny64 on a Raspberry Pi Pico 2 under TinyGo,
// which won't use this frontend at all, so nothing here should be
// depended on by anything outside cmd/*.
//
// The drawing lives in backend.go, which supplies two verbs, openDisplay
// and runLoop, and calls step once per frame. Everything else - the
// power-on noise, the reset sequence, the frame count - lives here, where
// the commands that share this frontend can rely on it being the same.
package desktop

import (
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/davecheney/tiny64"
)

const (
	// The picture the VIC-II actually emits: the blanking intervals are
	// never written, so there's no point sizing the window for them.
	ScreenWidth  = tiny64.VisibleDotsPerLine
	ScreenHeight = tiny64.VisibleLines

	// Scale is the smallest the window will open at, in window pixels per
	// emulated dot. One is too small to read the 40-column screen on
	// anything modern, so two is the floor; openingScale in backend.go
	// picks the actual opening size, which on a large or dense display is
	// a good deal more than this.
	Scale = 2

	// phi2Hz is the PAL 6569's dot clock divided down to Phi2: the rate
	// the CPU and the VIC-II both run at on a PAL machine.
	phi2Hz = 985248

	// palFramePeriod is how long one frame lasts on that machine -
	// CyclesPerFrame at phi2Hz, which is 19.95ms, or 50.125 frames a
	// second. It is what the loop paces to.
	palFramePeriod = time.Second * tiny64.CyclesPerFrame / phi2Hz
)

// frames counts the PAL frames the emulator has run, reported on exit as
// a rough check that the machine was actually executing.
var frames int

// pacer holds the emulated machine to PAL's frame rate.
//
// The display cannot do this job. Presenting with vsync paces the loop to
// whatever the panel refreshes at, which is nobody's 50.125Hz: a 60Hz
// screen ran the machine 1.2x fast, and a 120Hz one would run it 2.4x.
// That is the whole reason this exists - the cadence, not the cost.
//
// It does not buy back the CPU SDL spends waiting, and it was measured
// rather than assumed. With vsync on, the loop sat in SDL_RenderPresent
// and SDL_LockTexture for about a quarter of its samples; with vsync off
// and this sleeping instead, RenderPresent falls to nearly nothing and
// LockTexture rises to take its place, because what either of them waits
// for is the GPU releasing the staging buffer. Total process CPU comes
// out around a seventh of one core either way, for 50 frames a second
// now rather than 60. Swapping the upload to SDL_UpdateTexture on a
// STATIC texture measured the same again, so the wait is the GPU's and
// not the API's.
type pacer struct {
	// next is when the frame now being emulated should end. Zero until
	// the first frame, which is what starts the clock.
	next time.Time
}

// advance closes the frame that ended at now and answers how long to
// sleep before starting the next one - not positive if the frame overran,
// which time.Sleep treats as no wait at all.
//
// The deadline moves on by exactly one frame each time rather than being
// measured from now, so the jitter in any one sleep does not accumulate:
// sleeping 21ms once takes the next sleep down to 19, not the whole run
// out of step.
func (p *pacer) advance(now time.Time) time.Duration {
	if p.next.IsZero() {
		p.next = now
	}
	p.next = p.next.Add(palFramePeriod)

	left := p.next.Sub(now)
	if left <= 0 {
		// The frame overran, so it has already spent the time the next
		// one was going to have. Write the debt off rather than run the
		// following frames back to back to repay it: a machine that
		// stutters for a moment is better than one that then runs fast
		// to catch up.
		p.next = now
	}
	return left
}

// step advances the machine by one frame. runLoop calls it once per
// iteration and pacer decides how long that iteration lasts, so the
// emulated cadence is PAL's rather than the display's.
func step() {
	// Sample the host keyboard once per frame. The matrix itself is
	// combinational, so the guest sees whatever is held at the instant it
	// scans; this only bounds how often that state can change. At 50Hz
	// that is already finer than the KERNAL's own scan interval.
	pollKeyboard(tiny64.Keys())

	tiny64.StepFrame()
	frames++
}

// Run wires the VIC-II's pixel output to a window, randomizes RAM to
// simulate power-on noise, calls setup (if non-nil) so the caller can plug
// in a cartridge or a disk drive before reset, resets the machine, calls
// afterReset (if non-nil) so the caller can drive the freshly booted
// machine before the window takes over, and blocks running the frame loop
// until the window is closed.
//
// afterReset is where tiny64.Autostart goes. It runs the machine itself,
// so the frames it steps are not presented; by the time the loop below
// paints anything the prompt is up and the command has been typed.
func Run(title string, setup, afterReset func()) error {
	defer func() {
		fmt.Println("emulated frames:", frames)
	}()

	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}
	colorRAM := tiny64.ColorRam()
	for i := range colorRAM {
		colorRAM[i] = byte(rand.Uint() & 0x0F)
	}

	closeDisplay, err := openDisplay(title)
	if err != nil {
		return err
	}
	defer closeDisplay()

	if setup != nil {
		setup()
	}

	tiny64.Reset()

	if afterReset != nil {
		afterReset()
	}

	return runLoop()
}

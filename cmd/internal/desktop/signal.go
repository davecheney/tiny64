package desktop

import (
	"os"
	"os/signal"
	"syscall"
)

// Shutting down on a signal.
//
// SDL2 installed handlers for SIGINT and SIGTERM and turned them into a
// quit event, so ctrl-C in the terminal that started the emulator stopped
// it the same way closing the window did, and the frame count on the way
// out got printed either way. SDL3 still installs the handlers but no
// longer posts the event, which leaves the signal caught and acted on by
// nobody. So backend.go asks SDL not to install them at all, and these
// take over.
//
// The signal is not acted on where it arrives. It sets a flag that
// pumpEvents reads at the top of the next frame, so the shutdown runs on
// the same thread and through the same path as closing the window: runLoop
// returns, the deferred teardown destroys the window, and Run prints the
// frame count. Tearing SDL down from inside a handler would do none of
// that, and would do it on the wrong thread.
//
// This works under the standard toolchain. It does not under TinyGo,
// where SIGINT never arrives here: something in SDL_Init takes the signal
// regardless of the hint, which SDL_SetHint reports as having been set.
// That is not this code's doing - a TinyGo build with none of this in it
// ignores ctrl-C in exactly the same way - so what follows is an
// improvement where it works and inert where it does not. Closing the
// window stops the emulator under either compiler.

// quit is set when a signal has asked the emulator to stop. Buffered by
// one because the handler must never block, and one is enough: the second
// ctrl-C has nothing to add, and the loop only ever asks whether a
// shutdown was requested, not how often.
var quit = make(chan os.Signal, 1)

// notifyQuitSignals starts listening. It is called from openDisplay rather
// than an init function so that nothing is installed by merely importing
// this package - the commands that use it also run headless.
func notifyQuitSignals() {
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
}

// quitSignalled reports whether a shutdown has been asked for, without
// waiting for one.
func quitSignalled() bool {
	select {
	case <-quit:
		return true
	default:
		return false
	}
}

package desktop

import (
	"unsafe"

	"github.com/davecheney/tiny64"
)

// Every file that imports "C" needs its own #include to name the types it
// uses, but the #cgo flags are collected across the whole package, so they
// are stated once in backend_sdl.go and not repeated here. Repeating them
// works too, at the cost of a duplicate-library warning from the linker.

// #define SDL_MAIN_HANDLED
// #include <SDL.h>
import "C"

// The host keys, by physical position. See keyboard.go for why the map is
// positional rather than symbolic, and what the shifted table is for.
// keyboard_test.go checks the two tables between them cover the matrix.
var positional = map[C.SDL_Scancode]tiny64.Key{
	C.SDL_SCANCODE_A: tiny64.KeyA,
	C.SDL_SCANCODE_B: tiny64.KeyB,
	C.SDL_SCANCODE_C: tiny64.KeyC,
	C.SDL_SCANCODE_D: tiny64.KeyD,
	C.SDL_SCANCODE_E: tiny64.KeyE,
	C.SDL_SCANCODE_F: tiny64.KeyF,
	C.SDL_SCANCODE_G: tiny64.KeyG,
	C.SDL_SCANCODE_H: tiny64.KeyH,
	C.SDL_SCANCODE_I: tiny64.KeyI,
	C.SDL_SCANCODE_J: tiny64.KeyJ,
	C.SDL_SCANCODE_K: tiny64.KeyK,
	C.SDL_SCANCODE_L: tiny64.KeyL,
	C.SDL_SCANCODE_M: tiny64.KeyM,
	C.SDL_SCANCODE_N: tiny64.KeyN,
	C.SDL_SCANCODE_O: tiny64.KeyO,
	C.SDL_SCANCODE_P: tiny64.KeyP,
	C.SDL_SCANCODE_Q: tiny64.KeyQ,
	C.SDL_SCANCODE_R: tiny64.KeyR,
	C.SDL_SCANCODE_S: tiny64.KeyS,
	C.SDL_SCANCODE_T: tiny64.KeyT,
	C.SDL_SCANCODE_U: tiny64.KeyU,
	C.SDL_SCANCODE_V: tiny64.KeyV,
	C.SDL_SCANCODE_W: tiny64.KeyW,
	C.SDL_SCANCODE_X: tiny64.KeyX,
	C.SDL_SCANCODE_Y: tiny64.KeyY,
	C.SDL_SCANCODE_Z: tiny64.KeyZ,

	C.SDL_SCANCODE_0: tiny64.Key0,
	C.SDL_SCANCODE_1: tiny64.Key1,
	C.SDL_SCANCODE_2: tiny64.Key2,
	C.SDL_SCANCODE_3: tiny64.Key3,
	C.SDL_SCANCODE_4: tiny64.Key4,
	C.SDL_SCANCODE_5: tiny64.Key5,
	C.SDL_SCANCODE_6: tiny64.Key6,
	C.SDL_SCANCODE_7: tiny64.Key7,
	C.SDL_SCANCODE_8: tiny64.Key8,
	C.SDL_SCANCODE_9: tiny64.Key9,

	// The C64's top row runs "<- 1 2 3 4 5 6 7 8 9 0 + - GBP HOME DEL",
	// which is two keys wider than a PC's, so everything right of 0 is
	// shifted along by one position.
	C.SDL_SCANCODE_GRAVE:     tiny64.KeyLeftArrow,
	C.SDL_SCANCODE_MINUS:     tiny64.KeyPlus,
	C.SDL_SCANCODE_EQUALS:    tiny64.KeyMinus,
	C.SDL_SCANCODE_BACKSPACE: tiny64.KeyDelete,
	C.SDL_SCANCODE_HOME:      tiny64.KeyHome,
	// GBP sits between - and HOME, where a PC has no key at all, so it
	// has no positional home and gets End by convention.
	C.SDL_SCANCODE_END: tiny64.KeyPound,

	// Second row: the C64 has CTRL where a PC has Tab, and @ * ^ where a
	// PC has [ ] \.
	C.SDL_SCANCODE_TAB:          tiny64.KeyCtrl,
	C.SDL_SCANCODE_LEFTBRACKET:  tiny64.KeyAt,
	C.SDL_SCANCODE_RIGHTBRACKET: tiny64.KeyAsterisk,
	C.SDL_SCANCODE_BACKSLASH:    tiny64.KeyUpArrow,

	// Home row: the C64 has : and ; where a PC has ; and '.
	C.SDL_SCANCODE_SEMICOLON:  tiny64.KeyColon,
	C.SDL_SCANCODE_APOSTROPHE: tiny64.KeySemicolon,
	C.SDL_SCANCODE_RETURN:     tiny64.KeyReturn,
	C.SDL_SCANCODE_ESCAPE:     tiny64.KeyRunStop,
	// SHIFT LOCK is mechanically a latched LEFT SHIFT on real hardware,
	// and Caps Lock sits in the same spot.
	C.SDL_SCANCODE_CAPSLOCK: tiny64.KeyLShift,

	// Bottom rows. The C= key sits outboard of LEFT SHIFT, roughly where
	// a PC puts left Control.
	C.SDL_SCANCODE_COMMA:  tiny64.KeyComma,
	C.SDL_SCANCODE_PERIOD: tiny64.KeyPeriod,
	C.SDL_SCANCODE_SLASH:  tiny64.KeySlash,
	C.SDL_SCANCODE_LSHIFT: tiny64.KeyLShift,
	C.SDL_SCANCODE_RSHIFT: tiny64.KeyRShift,
	C.SDL_SCANCODE_LCTRL:  tiny64.KeyCommodore,
	C.SDL_SCANCODE_SPACE:  tiny64.KeySpace,

	C.SDL_SCANCODE_DOWN:  tiny64.KeyCursorDown,
	C.SDL_SCANCODE_RIGHT: tiny64.KeyCursorRight,

	C.SDL_SCANCODE_F1: tiny64.KeyF1,
	C.SDL_SCANCODE_F3: tiny64.KeyF3,
	C.SDL_SCANCODE_F5: tiny64.KeyF5,
	C.SDL_SCANCODE_F7: tiny64.KeyF7,
}

// shifted maps host keys that the C64 reaches by holding SHIFT rather than
// with a key of their own. See pressShifted in keyboard.go.
var shifted = map[C.SDL_Scancode]tiny64.Key{
	C.SDL_SCANCODE_UP:     tiny64.KeyCursorDown,
	C.SDL_SCANCODE_LEFT:   tiny64.KeyCursorRight,
	C.SDL_SCANCODE_INSERT: tiny64.KeyDelete,
	C.SDL_SCANCODE_F2:     tiny64.KeyF1,
	C.SDL_SCANCODE_F4:     tiny64.KeyF3,
	C.SDL_SCANCODE_F6:     tiny64.KeyF5,
	C.SDL_SCANCODE_F8:     tiny64.KeyF7,
}

// restoreHeld is whether RESTORE was down on the previous frame, which is
// how its press edge is recovered from level state. See pollKeyboard.
var restoreHeld bool

// pollKeyboard samples the host keyboard and rebuilds the C64 matrix to
// match. It rebuilds from scratch each frame rather than tracking press
// and release edges, so the matrix cannot drift out of step with the host
// if an event is missed or the window loses focus mid-keypress.
func pollKeyboard(keys *tiny64.Keyboard) {
	// SDL keeps one keyboard state array for the life of the program and
	// refreshes it as events are pumped, so this is a read of what
	// pumpEvents last drained rather than a fresh scan of the hardware.
	var n C.int
	held := unsafe.Slice((*byte)(unsafe.Pointer(C.SDL_GetKeyboardState(&n))), int(n))

	keys.ReleaseAll()

	for host, c64 := range positional {
		if held[host] != 0 {
			keys.Press(c64)
		}
	}

	for host, c64 := range shifted {
		if held[host] != 0 {
			pressShifted(keys, c64)
		}
	}

	// RESTORE is the one key that cannot be rebuilt from level state,
	// because it is not in the matrix: it triggers a monostable that
	// pulses the CPU's NMI pin once per press, so what it needs is the
	// press edge, not "is it down now". SDL has no just-pressed query, so
	// the edge is recovered by remembering the previous frame's level.
	// The emulated monostable owns the pulse width from there, so a
	// dropped release or a lost focus event cannot leave RESTORE jammed
	// the way a mishandled level key could.
	//
	// PageUp for want of a truly positional home: RESTORE sits above the
	// right-hand cursor keys on a C64, which is where the host's
	// navigation cluster sits relative to its arrows, and keeping it out
	// of the main block matches the fact that it is not a matrix key.
	restore := held[C.SDL_SCANCODE_PAGEUP] != 0
	if restore && !restoreHeld {
		keys.Restore()
	}
	restoreHeld = restore
}

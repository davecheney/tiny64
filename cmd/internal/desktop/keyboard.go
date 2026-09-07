package desktop

import (
	"github.com/davecheney/tiny64"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// This is a positional key map: host keys are matched to C64 keys by where
// they sit on the keyboard, not by the character they produce. Pressing
// the key to the right of L gives you the C64's ':' because that is what
// occupies that spot on a real C64, even though the host key cap says ';'.
//
// The alternative, a symbolic map, would translate by resulting character
// and synthesize SHIFT to reach symbols. That types BASIC more
// comfortably, but the synthesized SHIFT fights the user's real SHIFT key
// and lies to any program that scans the matrix directly instead of going
// through the KERNAL. Positional cannot misreport the matrix, so it is
// what we use.
//
// ebiten.Key is already defined in terms of physical US-layout key
// positions rather than the characters a layout produces, which is
// precisely the mapping domain we want.
var positional = map[ebiten.Key]tiny64.Key{
	ebiten.KeyA: tiny64.KeyA,
	ebiten.KeyB: tiny64.KeyB,
	ebiten.KeyC: tiny64.KeyC,
	ebiten.KeyD: tiny64.KeyD,
	ebiten.KeyE: tiny64.KeyE,
	ebiten.KeyF: tiny64.KeyF,
	ebiten.KeyG: tiny64.KeyG,
	ebiten.KeyH: tiny64.KeyH,
	ebiten.KeyI: tiny64.KeyI,
	ebiten.KeyJ: tiny64.KeyJ,
	ebiten.KeyK: tiny64.KeyK,
	ebiten.KeyL: tiny64.KeyL,
	ebiten.KeyM: tiny64.KeyM,
	ebiten.KeyN: tiny64.KeyN,
	ebiten.KeyO: tiny64.KeyO,
	ebiten.KeyP: tiny64.KeyP,
	ebiten.KeyQ: tiny64.KeyQ,
	ebiten.KeyR: tiny64.KeyR,
	ebiten.KeyS: tiny64.KeyS,
	ebiten.KeyT: tiny64.KeyT,
	ebiten.KeyU: tiny64.KeyU,
	ebiten.KeyV: tiny64.KeyV,
	ebiten.KeyW: tiny64.KeyW,
	ebiten.KeyX: tiny64.KeyX,
	ebiten.KeyY: tiny64.KeyY,
	ebiten.KeyZ: tiny64.KeyZ,

	ebiten.KeyDigit0: tiny64.Key0,
	ebiten.KeyDigit1: tiny64.Key1,
	ebiten.KeyDigit2: tiny64.Key2,
	ebiten.KeyDigit3: tiny64.Key3,
	ebiten.KeyDigit4: tiny64.Key4,
	ebiten.KeyDigit5: tiny64.Key5,
	ebiten.KeyDigit6: tiny64.Key6,
	ebiten.KeyDigit7: tiny64.Key7,
	ebiten.KeyDigit8: tiny64.Key8,
	ebiten.KeyDigit9: tiny64.Key9,

	// The C64's top row runs "<- 1 2 3 4 5 6 7 8 9 0 + - GBP HOME DEL",
	// which is two keys wider than a PC's, so everything right of 0 is
	// shifted along by one position.
	ebiten.KeyBackquote: tiny64.KeyLeftArrow,
	ebiten.KeyMinus:     tiny64.KeyPlus,
	ebiten.KeyEqual:     tiny64.KeyMinus,
	ebiten.KeyBackspace: tiny64.KeyDelete,
	ebiten.KeyHome:      tiny64.KeyHome,
	// GBP sits between - and HOME, where a PC has no key at all, so it
	// has no positional home and gets End by convention.
	ebiten.KeyEnd: tiny64.KeyPound,

	// Second row: the C64 has CTRL where a PC has Tab, and @ * ^ where a
	// PC has [ ] \.
	ebiten.KeyTab:          tiny64.KeyCtrl,
	ebiten.KeyBracketLeft:  tiny64.KeyAt,
	ebiten.KeyBracketRight: tiny64.KeyAsterisk,
	ebiten.KeyBackslash:    tiny64.KeyUpArrow,

	// Home row: the C64 has : and ; where a PC has ; and '.
	ebiten.KeySemicolon: tiny64.KeyColon,
	ebiten.KeyQuote:     tiny64.KeySemicolon,
	ebiten.KeyEnter:     tiny64.KeyReturn,
	ebiten.KeyEscape:    tiny64.KeyRunStop,
	// SHIFT LOCK is mechanically a latched LEFT SHIFT on real hardware,
	// and Caps Lock sits in the same spot.
	ebiten.KeyCapsLock: tiny64.KeyLShift,

	// Bottom rows. The C= key sits outboard of LEFT SHIFT, roughly where
	// a PC puts left Control.
	ebiten.KeyComma:       tiny64.KeyComma,
	ebiten.KeyPeriod:      tiny64.KeyPeriod,
	ebiten.KeySlash:       tiny64.KeySlash,
	ebiten.KeyShiftLeft:   tiny64.KeyLShift,
	ebiten.KeyShiftRight:  tiny64.KeyRShift,
	ebiten.KeyControlLeft: tiny64.KeyCommodore,
	ebiten.KeySpace:       tiny64.KeySpace,

	ebiten.KeyArrowDown:  tiny64.KeyCursorDown,
	ebiten.KeyArrowRight: tiny64.KeyCursorRight,

	ebiten.KeyF1: tiny64.KeyF1,
	ebiten.KeyF3: tiny64.KeyF3,
	ebiten.KeyF5: tiny64.KeyF5,
	ebiten.KeyF7: tiny64.KeyF7,
}

// shifted maps host keys that the C64 reaches by holding SHIFT rather
// than with a key of their own. Two different things end up here.
//
// Cursor up and left, and the even-numbered function keys, have no
// separate switch in the matrix at all: the C64 has only two cursor keys
// and four function keys, and shift is how you get the other direction
// and the other four. Insert is the opposite case, a key that does exist
// on the host and whose C64 equivalent is genuinely the shifted form of
// a switch that is already in the matrix, since INST and DEL are one
// physical key at PA0/PB0 and shift chooses between them.
//
// This is the one place the positional map synthesizes a modifier. For
// the cursor and function keys it is worth the impurity because the
// alternative is having no way at all to move the cursor up or left. In
// every case a program reading the matrix directly sees SHIFT held during
// these presses, exactly as it would if the user had pressed SHIFT
// themselves, which for Insert is precisely what the real machine does.
var shifted = map[ebiten.Key]tiny64.Key{
	ebiten.KeyArrowUp:   tiny64.KeyCursorDown,
	ebiten.KeyArrowLeft: tiny64.KeyCursorRight,
	ebiten.KeyInsert:    tiny64.KeyDelete,
	ebiten.KeyF2:        tiny64.KeyF1,
	ebiten.KeyF4:        tiny64.KeyF3,
	ebiten.KeyF6:        tiny64.KeyF5,
	ebiten.KeyF8:        tiny64.KeyF7,
}

// pollKeyboard samples the host keyboard and rebuilds the C64 matrix to
// match. It rebuilds from scratch each frame rather than tracking press
// and release edges, so the matrix cannot drift out of step with the host
// if an event is missed or the window loses focus mid-keypress.
func pollKeyboard(keys *tiny64.Keyboard) {
	keys.ReleaseAll()

	for host, c64 := range positional {
		if ebiten.IsKeyPressed(host) {
			keys.Press(c64)
		}
	}

	for host, c64 := range shifted {
		if ebiten.IsKeyPressed(host) {
			keys.Press(c64)
			keys.Press(tiny64.KeyLShift)
		}
	}

	// RESTORE is the one key that cannot be rebuilt from level state,
	// because it is not in the matrix: it triggers a monostable that
	// pulses the CPU's NMI pin once per press, so what it needs is the
	// press edge, not "is it down now". IsKeyJustPressed supplies exactly
	// that, and the emulated monostable owns the pulse width from there,
	// so a dropped release or a lost focus event cannot leave RESTORE
	// jammed the way a mishandled level key could.
	//
	// PageUp for want of a truly positional home: RESTORE sits above the
	// right-hand cursor keys on a C64, which is where the host's
	// navigation cluster sits relative to its arrows, and keeping it out
	// of the main block matches the fact that it is not a matrix key.
	if inpututil.IsKeyJustPressed(ebiten.KeyPageUp) {
		keys.Restore()
	}
}

package desktop

import "github.com/davecheney/tiny64"

// This is where the reasoning behind the host keyboard map lives; the map
// itself is in keymap.go, over SDL's scancodes.
//
// The map is positional: host keys are matched to C64 keys by where they
// sit on the keyboard, not by the character they produce. Pressing the key
// to the right of L gives you the C64's ':' because that is what occupies
// that spot on a real C64, even though the host key cap says ';'.
//
// The alternative, a symbolic map, would translate by resulting character
// and synthesize SHIFT to reach symbols. That types BASIC more
// comfortably, but the synthesized SHIFT fights the user's real SHIFT key
// and lies to any program that scans the matrix directly instead of going
// through the KERNAL. Positional cannot misreport the matrix, so it is
// what we use.
//
// An SDL scancode suits that exactly: it names a physical key position
// rather than the character the host's layout produces from it. So
// keymap.go is a positional table and a shifted table over SDL_Scancode,
// and a pollKeyboard that rebuilds the matrix from them.

// pressShifted presses a C64 key that the host reaches only by holding
// SHIFT, synthesizing the modifier.
//
// Two different things need this. Cursor up and left, and the
// even-numbered function keys, have no separate switch in the matrix at
// all: the C64 has only two cursor keys and four function keys, and shift
// is how you get the other direction and the other four. Insert is the
// opposite case, a key that does exist on the host and whose C64
// equivalent is genuinely the shifted form of a switch already in the
// matrix, since INST and DEL are one physical key at PA0/PB0 and shift
// chooses between them.
//
// This is the one place the positional map synthesizes a modifier. For the
// cursor and function keys it is worth the impurity because the
// alternative is having no way at all to move the cursor up or left. In
// every case a program reading the matrix directly sees SHIFT held during
// these presses, exactly as it would if the user had pressed SHIFT
// themselves, which for Insert is precisely what the real machine does.
func pressShifted(keys *tiny64.Keyboard, key tiny64.Key) {
	keys.Press(key)
	keys.Press(tiny64.KeyLShift)
}

package tiny64

import "testing"

// The unit tests in keyboard_test.go check the matrix in isolation, by
// driving the port lines by hand. These check the whole path instead: a
// real KERNAL boots, its Timer A interrupt calls the scan routine at
// $EA87, that routine walks Port A and reads Port B through ioLoad, and
// whatever it decodes ends up in the keyboard buffer for BASIC to read.
// Nothing here knows how the matrix is wired; it only knows what should
// appear on the screen, so it stays honest if the implementation changes.

// TestKeyboardTypesAtBASICPrompt boots the real KERNAL and types
// PRINT"HELLO" at the READY prompt, then checks both that the line echoed
// as typed and that BASIC ran it. The repeated L exercises debounce, since
// a matrix that never reported a release would collapse it to one letter,
// and the quotes exercise SHIFT, which the KERNAL finds by reverse-
// scanning the matrix rather than by walking Port A.
func TestKeyboardTypesAtBASICPrompt(t *testing.T) {
	m := newMachine(t)

	m.waitForLine(5, "READY.")

	// " is SHIFT+2 on a C64.
	for _, k := range []struct {
		shift bool
		key   Key
	}{
		{false, KeyP}, {false, KeyR}, {false, KeyI}, {false, KeyN}, {false, KeyT},
		{true, Key2},
		{false, KeyH}, {false, KeyE}, {false, KeyL}, {false, KeyL}, {false, KeyO},
		{true, Key2},
		{false, KeyReturn},
	} {
		m.press(k.shift, k.key)
	}

	if got := screenLine(6); got != `PRINT"HELLO"` {
		t.Errorf("typed line = %q, want %q", got, `PRINT"HELLO"`)
	}
	m.waitForLine(7, "HELLO")
}

// TestKeyboardIdleTypesNothing is the control for the test above: the same
// boot, the same number of cycles, but with nothing ever pressed. If the
// matrix reported phantom presses at rest, the prompt line would not stay
// empty.
func TestKeyboardIdleTypesNothing(t *testing.T) {
	m := newMachine(t)

	m.waitForLine(5, "READY.")
	m.run(13 * 2 * cyclesPerKeyPhase)

	if got := screenLine(6); got != "" {
		t.Errorf("with no keys held, prompt line = %q, want it empty", got)
	}
}

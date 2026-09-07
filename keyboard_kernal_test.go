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

// RUN/STOP plus RESTORE is the C64's warm reset gesture, and it works
// through a path that has nothing to do with the matrix scan: RESTORE
// fires a monostable that pulses the CPU's NMI pin, the KERNAL's NMI
// handler finds no CIA2 interrupt to blame it on, checks the STOP key
// itself, and on finding it held reinitializes I/O and the screen editor
// before jumping to BASIC's warm start. The visible result is a cleared
// screen with a fresh READY, without the machine having gone through a
// cold boot: the sign-on banner and the free-memory line do not come back.
func TestRestoreWithRunStopWarmResets(t *testing.T) {
	m := newMachine(t)

	m.waitForLine(5, "READY.")
	for _, k := range []Key{KeyH, KeyE, KeyL, KeyO} {
		m.press(false, k)
	}
	if got := screenLine(6); got != "HELO" {
		t.Fatalf("before RESTORE, prompt line = %q, want %q", got, "HELO")
	}

	keyboard.Press(KeyRunStop)
	m.run(cyclesPerKeyPhase)
	keyboard.Restore()
	m.run(cyclesPerKeyPhase)
	keyboard.ReleaseAll()
	m.run(cyclesPerKeyPhase)

	if got := screenLine(1); got != "READY." {
		t.Errorf("after RUN/STOP+RESTORE, line 1 = %q, want %q", got, "READY.")
	}
	for _, row := range []int{2, 3, 5, 6} {
		if got := screenLine(row); got != "" {
			t.Errorf("after RUN/STOP+RESTORE, line %d = %q, want it cleared", row, got)
		}
	}
}

// RESTORE on its own takes the same NMI, but the handler finds STOP not
// held and returns through RTI, so nothing is disturbed: the half-typed
// line is still sitting at the prompt afterwards. That is the control for
// the test above, which would otherwise be satisfied by any accidental
// reset.
func TestRestoreAloneDisturbsNothing(t *testing.T) {
	m := newMachine(t)

	m.waitForLine(5, "READY.")
	for _, k := range []Key{KeyH, KeyE, KeyL, KeyO} {
		m.press(false, k)
	}

	keyboard.Restore()
	m.run(cyclesPerKeyPhase)

	if got := screenLine(5); got != "READY." {
		t.Errorf("after RESTORE alone, line 5 = %q, want %q", got, "READY.")
	}
	if got := screenLine(6); got != "HELO" {
		t.Errorf("after RESTORE alone, prompt line = %q, want %q", got, "HELO")
	}

	// And the machine is still alive: typing continues where it left off.
	m.press(false, KeyReturn)
	m.waitForLine(8, "?SYNTAX  ERROR")
}

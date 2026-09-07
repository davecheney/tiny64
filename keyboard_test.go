package tiny64

import "testing"

// scanPB drives a single Port A line low (the way the KERNAL does) and
// returns what Port B reads back, with CIA1 configured the standard way:
// PA all outputs, PB all inputs.
func scanPB(k *Keyboard, paLow int) uint8 {
	pa := uint8(0xFF) &^ (1 << paLow)
	_, pb := k.scan(pa, 0xFF)
	return pb
}

func TestKeyboardIdleReadsAllHigh(t *testing.T) {
	var k Keyboard
	for row := range 8 {
		if got := scanPB(&k, row); got != 0xFF {
			t.Errorf("PA%d low, no keys held: PB = %#02x, want 0xff", row, got)
		}
	}
}

// A pressed key must only pull its PB line low while its own PA line is
// the one being driven low; every other row must read clean.
func TestKeyboardSelectsOnlyItsOwnRow(t *testing.T) {
	var k Keyboard
	k.Press(KeyA) // PA1, PB2

	for row := range 8 {
		got := scanPB(&k, row)
		want := uint8(0xFF)
		if row == int(KeyA.Row()) {
			want &^= 1 << KeyA.Col()
		}
		if got != want {
			t.Errorf("PA%d low with A held: PB = %#02x, want %#02x", row, got, want)
		}
	}
}

func TestKeyboardReleaseClearsKey(t *testing.T) {
	var k Keyboard
	k.Press(KeyA)
	k.Release(KeyA)

	if got := scanPB(&k, int(KeyA.Row())); got != 0xFF {
		t.Errorf("after release: PB = %#02x, want 0xff", got)
	}
}

func TestKeyboardReleaseAll(t *testing.T) {
	var k Keyboard
	k.Press(KeyA)
	k.Press(KeyRunStop)
	k.Press(KeyLShift)
	k.ReleaseAll()

	for row := range 8 {
		if got := scanPB(&k, row); got != 0xFF {
			t.Errorf("PA%d low after ReleaseAll: PB = %#02x, want 0xff", row, got)
		}
	}
}

// Two keys sharing a PA line must both show up in the same scan step.
func TestKeyboardTwoKeysSameRow(t *testing.T) {
	var k Keyboard
	k.Press(Key3) // PA1, PB0
	k.Press(KeyW) // PA1, PB1

	want := uint8(0xFF) &^ 0x03
	if got := scanPB(&k, 1); got != want {
		t.Errorf("PB = %#02x, want %#02x", got, want)
	}
}

// Every key must be reachable, and land on exactly the row/column the
// matrix table claims. This guards the whole constant block against typos,
// which are otherwise silent and produce baffling wrong characters.
func TestKeyboardEveryKeyIsDistinct(t *testing.T) {
	all := []Key{
		KeyDelete, KeyReturn, KeyCursorRight, KeyF7, KeyF1, KeyF3, KeyF5, KeyCursorDown,
		Key3, KeyW, KeyA, Key4, KeyZ, KeyS, KeyE, KeyLShift,
		Key5, KeyR, KeyD, Key6, KeyC, KeyF, KeyT, KeyX,
		Key7, KeyY, KeyG, Key8, KeyB, KeyH, KeyU, KeyV,
		Key9, KeyI, KeyJ, Key0, KeyM, KeyK, KeyO, KeyN,
		KeyPlus, KeyP, KeyL, KeyMinus, KeyPeriod, KeyColon, KeyAt, KeyComma,
		KeyPound, KeyAsterisk, KeySemicolon, KeyHome, KeyRShift, KeyEquals, KeyUpArrow, KeySlash,
		Key1, KeyLeftArrow, KeyCtrl, Key2, KeySpace, KeyCommodore, KeyQ, KeyRunStop,
	}
	if len(all) != 64 {
		t.Fatalf("matrix has %d keys, want 64", len(all))
	}

	seen := map[Key]bool{}
	for _, key := range all {
		if seen[key] {
			t.Errorf("key %#02x (PA%d/PB%d) is listed twice", uint8(key), key.Row(), key.Col())
		}
		seen[key] = true

		var k Keyboard
		k.Press(key)
		want := uint8(0xFF) &^ (1 << key.Col())
		if got := scanPB(&k, int(key.Row())); got != want {
			t.Errorf("key %#02x: PB = %#02x, want %#02x", uint8(key), got, want)
		}
	}
}

// The KERNAL detects SHIFT/CTRL/C= by scanning backwards: it drives Port B
// and reads Port A. The matrix is just wire, so this has to work.
func TestKeyboardReverseScan(t *testing.T) {
	var k Keyboard
	k.Press(KeyLShift) // PA1, PB7

	// Drive PB7 low, read PA. CIA1 is reversed here: PB output, PA input.
	pa, _ := k.scan(0xFF, 0xFF&^(1<<KeyLShift.Col()))

	want := uint8(0xFF) &^ (1 << KeyLShift.Row())
	if pa != want {
		t.Errorf("reverse scan: PA = %#02x, want %#02x", pa, want)
	}
}

// Three keys forming three corners of a rectangle in the matrix complete a
// path to the fourth corner, so real hardware reports a key that is not
// being held. Reproducing this is the point of resolving the matrix
// iteratively rather than in a single pass.
func TestKeyboardGhosting(t *testing.T) {
	// A (PA1/PB2), S (PA1/PB5) and D (PA2/PB2) form three corners; the
	// phantom is PA2/PB5, which is F.
	var k Keyboard
	k.Press(KeyA)
	k.Press(KeyS)
	k.Press(KeyD)

	got := scanPB(&k, int(KeyF.Row()))
	if got&(1<<KeyF.Col()) != 0 {
		t.Errorf("PA%d low: PB = %#02x, expected phantom F (PB%d) to read low",
			KeyF.Row(), got, KeyF.Col())
	}

	// Lifting any one corner must break the path and clear the phantom.
	k.Release(KeyS)
	got = scanPB(&k, int(KeyF.Row()))
	if got&(1<<KeyF.Col()) == 0 {
		t.Errorf("PA%d low after releasing S: PB = %#02x, phantom F should be gone",
			KeyF.Row(), got)
	}
}

// With no PA line driven low there is nothing to pull PB down, however
// many keys are held.
func TestKeyboardNoRowSelectedReadsHigh(t *testing.T) {
	var k Keyboard
	k.Press(KeyA)
	k.Press(KeyQ)
	k.Press(KeySpace)

	if pa, pb := k.scan(0xFF, 0xFF); pa != 0xFF || pb != 0xFF {
		t.Errorf("nothing driven: PA = %#02x, PB = %#02x, want both 0xff", pa, pb)
	}
}

// The whole point is that software reads this through CIA1's registers, so
// exercise the real path: $DC00/$DC01 via the PLA, configured the way the
// KERNAL configures them.
func TestKeyboardThroughCIA1Registers(t *testing.T) {
	keyboard.ReleaseAll()
	defer keyboard.ReleaseAll()

	cia1.Store(0xDC02, 0xFF) // DDRA: Port A all outputs
	cia1.Store(0xDC03, 0x00) // DDRB: Port B all inputs

	keyboard.Press(KeyQ) // PA7, PB6

	cia1.Store(0xDC00, 0xFF&^(1<<KeyQ.Row()))
	want := uint8(0xFF) &^ (1 << KeyQ.Col())
	if got := ioLoad(0xDC01); got != want {
		t.Errorf("$DC01 = %#02x, want %#02x", got, want)
	}

	// A different row must not see it.
	cia1.Store(0xDC00, 0xFF&^0x01)
	if got := ioLoad(0xDC01); got != 0xFF {
		t.Errorf("$DC01 with wrong row selected = %#02x, want 0xff", got)
	}

	// And with no key held at all, the selected row reads clean.
	keyboard.Release(KeyQ)
	cia1.Store(0xDC00, 0xFF&^(1<<KeyQ.Row()))
	if got := ioLoad(0xDC01); got != 0xFF {
		t.Errorf("$DC01 with key released = %#02x, want 0xff", got)
	}
}

// Port A must still read back normally when no key bridges it, otherwise
// software that uses $DC00 for anything else (joystick, or just reading
// back what it wrote) breaks.
func TestKeyboardPortAReadbackUnaffected(t *testing.T) {
	keyboard.ReleaseAll()
	defer keyboard.ReleaseAll()

	cia1.Store(0xDC02, 0xFF)
	cia1.Store(0xDC03, 0x00)
	cia1.Store(0xDC00, 0xAA)

	if got := ioLoad(0xDC00); got != 0xAA {
		t.Errorf("$DC00 = %#02x, want 0xaa", got)
	}
}

package tiny64

import (
	"strings"
	"testing"
)

// The unit tests in keyboard_test.go check the matrix in isolation, by
// driving the port lines by hand. This one checks the whole path instead:
// a real KERNAL boots, its Timer A interrupt calls the scan routine at
// $EA87, that routine walks Port A and reads Port B through ioLoad, and
// whatever it decodes ends up in the keyboard buffer for BASIC to read.
// Nothing here knows how the matrix is wired; it only knows what should
// appear on the screen, so it stays honest if the implementation changes.

// screenLine reads one 40-column row of the default screen RAM at $0400
// and converts it back from screen codes to ASCII. Codes $00-$1F are the
// letters and @[]-style symbols, which sit 64 positions lower than their
// ASCII equivalents; $20-$3F are punctuation and digits, which match
// ASCII already. Bit 7 is the reverse-video flag, which the cursor sets on
// whichever character it is sitting on, so it is masked off to keep the
// result independent of where the blink happens to be.
func screenLine(row int) string {
	var b strings.Builder
	for col := range 40 {
		c := ram[0x0400+row*40+col] & 0x7F
		switch {
		case c <= 0x1F:
			b.WriteByte(c + 0x40)
		case c <= 0x3F:
			b.WriteByte(c)
		default:
			b.WriteByte('?')
		}
	}
	return strings.TrimRight(b.String(), " ")
}

// A key has to stay down long enough for the KERNAL to see it on one scan
// and still be down on the next, and stay up long enough for the release
// to register, or the debounce logic either drops the press or repeats it.
// SCNKEY runs off CIA1 Timer A at roughly 60Hz, so about 16400 cycles per
// scan; 40000 cycles is a comfortable two to three scans either way,
// which is also about how long a human holds a key.
const cyclesPerKeyPhase = 40_000

type machine struct{ t *testing.T }

// newMachine builds a cold C64. Reset only reloads PC from the reset
// vector and reinitializes the video logic, exactly as the hardware RESET
// line does, so it will happily resume a half-executed instruction left
// behind by an earlier test in this process. Everything the machine is
// made of is a package-level singleton, so each part is put back to its
// power-on state explicitly rather than trusted.
//
// The same singletons are shared with every other test in the package,
// and some of those tests both step the CPU and depend on state an
// earlier test left behind, so the previous contents are saved and put
// back afterwards. Booting a whole machine here then stays invisible to
// anything that runs later.
func newMachine(t *testing.T) *machine {
	savedCPU, savedCIA1, savedCIA2 := cpu, cia1, cia2
	savedKeyboard, savedVIC, savedCartridge := keyboard, vic, cartridge
	savedRAM := ram
	t.Cleanup(func() {
		cpu, cia1, cia2 = savedCPU, savedCIA1, savedCIA2
		keyboard, vic, cartridge = savedKeyboard, savedVIC, savedCartridge
		ram = savedRAM
	})

	cpu = CPU{} // Port and PortDDR zero: all port pins float high, which is the default banking
	cia1 = CIA{}
	cia2 = CIA{}
	keyboard = Keyboard{}
	bus.Remove() // no cartridge, so the KERNAL gets the reset vector
	vic = VICII{}
	for i := range ram {
		ram[i] = 0
	}
	// The VIC-II will not step without somewhere to put its pixels, and
	// there is no display here, so throw them away.
	vic.WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	Reset()
	return &machine{t: t}
}

// run advances the machine by n CPU cycles. One CPU cycle is eight VIC-II
// dots, and the VIC-II is what drives the CPU, so dots are the unit that
// actually ticks.
func (m *machine) run(cycles int) {
	for range cycles * 8 {
		vic.StepDot()
	}
}

// press holds a key, optionally with SHIFT, then lets go. It releases
// everything rather than just the key it pressed, which is what a front
// end rebuilding the matrix each frame effectively does.
func (m *machine) press(shift bool, key Key) {
	if shift {
		keyboard.Press(KeyLShift)
	}
	keyboard.Press(key)
	m.run(cyclesPerKeyPhase)
	keyboard.ReleaseAll()
	m.run(cyclesPerKeyPhase)
}

// waitForLine runs the machine until the given screen row starts with
// want, giving up after a generous budget. Booting to the BASIC prompt
// takes around two million cycles, or a couple of seconds of C64 time.
func (m *machine) waitForLine(row int, want string) {
	m.t.Helper()
	const budget = 4_000_000
	for spent := 0; spent < budget; spent += 100_000 {
		if strings.HasPrefix(screenLine(row), want) {
			return
		}
		m.run(100_000)
	}
	m.t.Fatalf("after %d cycles, line %d = %q, want it to start with %q", budget, row, screenLine(row), want)
}

// TestKeyboardTypesAtBASICPrompt boots the real KERNAL and types
// PRINT"HELLO" at the READY prompt, then checks both that the line echoed
// as typed and that BASIC ran it. The repeated L exercises debounce, since
// a matrix that never reports a release would collapse it to one letter,
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

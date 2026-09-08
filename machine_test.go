package tiny64

import (
	"bytes"
	"strings"
	"testing"
)

// The emulator is built out of package-level singletons: one CPU, one
// VIC-II, two CIAs, one 64K RAM array, one colour RAM chip, one cartridge
// slot. That is the right shape for a machine that only ever has one of
// each, but it means every test in this package shares them, so a test
// that leaves dirty state behind surfaces as a failure in some unrelated
// test that happens to run later. The helpers here keep that contained.

// saveMachine snapshots every one of those singletons and puts them back
// when the test finishes, so a test is free to clobber whatever it needs
// to without having to know who runs next.
func saveMachine(t *testing.T) {
	savedCPU, savedCIA1, savedCIA2 := cpu, cia1, cia2
	savedKeyboard, savedVIC, savedCartridge := keyboard, vic, cartridge
	savedRAM, savedColorRAM := ram, colorRAM
	savedDriveCPU, savedVIA1, savedVIA2 := driveCPU, via1, via2
	savedDriveRAM, savedDisk := driveRAM, diskImage
	savedDriveAttached := driveAttached
	// The bus is its own state, not a shadow of driveAttached: a peripheral
	// can sit on it without setting that flag at all. Snapshot the slice
	// header and its contents, since a test may replace an entry.
	savedBus := append([]iecPeripheral(nil), iecBus...)
	t.Cleanup(func() {
		cpu, cia1, cia2 = savedCPU, savedCIA1, savedCIA2
		keyboard, vic, cartridge = savedKeyboard, savedVIC, savedCartridge
		ram, colorRAM = savedRAM, savedColorRAM
		driveCPU, via1, via2 = savedDriveCPU, savedVIA1, savedVIA2
		driveRAM = savedDriveRAM
		InsertDisk(savedDisk)
		driveResetDisk()
		driveAttached = savedDriveAttached
		// Last, because InsertDisk above attaches a 1541 of its own.
		iecBus = savedBus
	})
}

func clearFrameBufferRGBA() {
	clear(FrameBufferRGBA())
}

func frameBufferPixelRGBA(x, y uint16) []byte {
	const stride = VisibleDotsPerLine * 4
	idx := int(y-FirstVisibleLine)*stride + int(x)*4
	return FrameBufferRGBA()[idx : idx+4]
}

func frameBufferPixelIs(x, y uint16, colorIndex byte) bool {
	return bytes.Equal(frameBufferPixelRGBA(x, y), C64Palette[colorIndex&0x0f][:])
}

// A key has to stay down long enough for the KERNAL to see it on one scan
// and still be down on the next, and stay up long enough for the release
// to register, or the debounce logic either drops the press or repeats it.
// SCNKEY runs off CIA1 Timer A at roughly 60Hz, so about 16400 cycles per
// scan; 40000 cycles is a comfortable two to three scans either way,
// which is also about how long a human holds a key.
const cyclesPerKeyPhase = 40_000

// machine is a whole emulated C64 running a real KERNAL, for tests that
// want to check behaviour end to end rather than poke at one chip.
type machine struct{ t *testing.T }

// newMachine builds a cold C64. Reset only reloads PC from the reset
// vector and reinitializes the video logic, exactly as the hardware RESET
// line does, so on its own it will happily resume a half-executed
// instruction left behind by an earlier test. Every part is therefore put
// back to its power-on state explicitly rather than trusted.
func newMachine(t *testing.T) *machine {
	saveMachine(t)

	cpu = CPU{} // Port and PortDDR zero: all port pins float high, which is the default banking
	cia1 = CIA{}
	cia2 = CIA{}
	keyboard = Keyboard{}
	bus.Remove() // no cartridge, so the KERNAL gets the reset vector
	vic = VICII{}
	for i := range ram {
		ram[i] = 0
	}
	colorRAM = [1024]byte{}
	clearFrameBufferRGBA()
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

// typeLine types a line of BASIC and presses RETURN, the way someone
// sitting at the machine would. Only the characters the tests need are
// mapped; anything else is a bug in the test, not something the emulated
// keyboard should be asked to guess at.
func (m *machine) typeLine(line string) {
	m.t.Helper()
	for _, r := range line {
		shift, key, ok := keyFor(r)
		if !ok {
			m.t.Fatalf("no key mapping for %q", r)
		}
		m.press(shift, key)
	}
	m.press(false, KeyReturn)
}

// keyFor maps an ASCII character to the key (and SHIFT state) that
// produces it on a C64 keyboard.
func keyFor(r rune) (shift bool, key Key, ok bool) {
	if r >= 'A' && r <= 'Z' {
		return false, letterKeys[r-'A'], true
	}
	if r >= '0' && r <= '9' {
		return false, digitKeys[r-'0'], true
	}
	switch r {
	case ' ':
		return false, KeySpace, true
	case ',':
		return false, KeyComma, true
	case ':':
		return false, KeyColon, true
	case ';':
		return false, KeySemicolon, true
	case '+':
		return false, KeyPlus, true
	case '-':
		return false, KeyMinus, true
	case '.':
		return false, KeyPeriod, true
	case '/':
		return false, KeySlash, true
	case '*':
		return false, KeyAsterisk, true
	case '=':
		return false, KeyEquals, true
	case '@':
		return false, KeyAt, true
	case '#':
		return true, Key3, true
	case '$':
		return true, Key4, true
	case '"':
		return true, Key2, true
	case '(':
		return true, Key8, true
	case ')':
		return true, Key9, true
	}
	return false, 0, false
}

var (
	letterKeys = [26]Key{
		KeyA, KeyB, KeyC, KeyD, KeyE, KeyF, KeyG, KeyH, KeyI,
		KeyJ, KeyK, KeyL, KeyM, KeyN, KeyO, KeyP, KeyQ, KeyR,
		KeyS, KeyT, KeyU, KeyV, KeyW, KeyX, KeyY, KeyZ,
	}
	digitKeys = [10]Key{Key0, Key1, Key2, Key3, Key4, Key5, Key6, Key7, Key8, Key9}
)

// waitForLine runs the machine until the given screen row starts with
// want, giving up after a generous budget. Booting to the BASIC prompt
// takes around two million cycles, or a couple of seconds of C64 time.
// reset restarts the machine and waits for the KERNAL to repaint the
// screen, which is not the same as waiting for the prompt to appear.
// Reset() does not clear screen RAM, and the KERNAL spends over two
// million cycles counting memory before it writes anything, so whatever
// was on the screen beforehand - including a "READY." from the previous
// boot - stays there and will satisfy waitForLine immediately. A test
// that waits for the prompt after a reset can therefore carry on typing
// into a machine that is still in its RAM test, and pass or fail for
// reasons that have nothing to do with what it meant to check. Blanking
// the screen first means the text that gets waited for has to be text
// this boot actually produced.
func (m *machine) reset(row int, want string) {
	m.t.Helper()
	for i := range 1000 {
		ram[0x0400+i] = 0x20 // space
	}
	Reset()
	m.waitForLine(row, want)
}

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

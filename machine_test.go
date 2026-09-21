package tiny64

import (
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
	savedCPU, savedCIA := cpu, cia
	savedKeyboard, savedVIC, savedCartridge := keyboard, vic, cartridge
	savedRAM, savedColorRAM := ram, colorRAM
	savedDisk := diskImage
	// Whatever drive this build has is saved behind the seam, because a
	// build without the 1541 has no drive CPU, VIAs or drive RAM to put
	// back. The disk goes back through the same closure so that the order
	// - drive state, then the disk, then the track - is stated in one
	// place rather than split across two files.
	restoreDrive := saveDriveState()
	// The bus is its own state, not a shadow of any drive's: a peripheral
	// can sit on it without a drive being attached at all. Snapshot the
	// slice header and its contents, since a test may replace an entry.
	savedBus := append([]iecPeripheral(nil), iecBus...)
	t.Cleanup(func() {
		cpu, cia = savedCPU, savedCIA
		keyboard, vic, cartridge = savedKeyboard, savedVIC, savedCartridge
		ram, colorRAM = savedRAM, savedColorRAM
		restoreDrive(savedDisk)
		// Last, because InsertDisk above attaches a drive of its own.
		iecBus = savedBus
	})
}

// skipShort skips a test under -short.
//
// The 1541 and IEC drive tests are the expensive ones: each boots a whole
// emulated C64, waits for the KERNAL to come up, and then talks to a
// drive one bus transition at a time, which costs tens of millions of
// emulated cycles. Together they are most of the suite's runtime, so
// `go test -short .` gives a fast local run of everything else. CI runs
// without -short, so they still guard every change.
func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("boots an emulated machine and drives a disk drive; skipped under -short")
	}
}

// setVICBank drives CIA2's port A to select one of the VIC-II's four 16K
// fetch windows, and moves the window with it.
//
// Both halves matter. The two bank-select lines are inverted by the board
// logic, and the window the VIC fetches through is worked out where a
// write to this port lands, so a test that assigns the port fields alone
// gets an uninverted bank and a window still describing the previous one.
// Bank 0 hides that: it is what a zeroed VICII already holds, so poking
// the fields for bank 0 passes whether the window tracks the port or not.
func (c *CIA) setVICBank(bank uint8) {
	c.cia2.DDRA |= 0x03
	c.cia2.PRA = c.cia2.PRA&^0x03 | ^bank&0x03
	vic.setBank()
}

func frameBufferPixelIs(x, y uint16, colorIndex byte) bool {
	return frameBufferPixelRGBA(x, y) == C64Palette[colorIndex&0x0f]
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

// coldStart puts every singleton back to its power-on state, stopping
// short of pulling the RESET line. Reset only reloads PC from the reset
// vector and reinitializes the video logic, exactly as the hardware RESET
// line does, so on its own it will happily resume a half-executed
// instruction left behind by an earlier test - or, worse, read the reset
// vector through whatever banking that test left in the 6510's port,
// which is RAM rather than the KERNAL for most of the CPU tests. Every
// part is therefore put back explicitly rather than trusted.
//
// This is separate from newMachine because the two orders a front end can
// use - reset then insert a disk, or insert a disk then reset - need the
// same cold machine underneath but differ in what happens between the
// power-on state and the RESET, so a test doing it the second way cannot
// simply call newMachine.
func coldStart() {
	cpu = CPU{} // Port and PortDDR zero: all port pins float high, which is the default banking
	cia = CIA{}
	keyboard = Keyboard{}
	bus.Remove() // no cartridge, so the KERNAL gets the reset vector
	vic = VICII{}
	for i := range ram {
		ram[i] = 0
	}
	colorRAM = [1024]byte{}
	ClearFrameBuffer()
}

// cpuParkAddr is where parkCPU leaves the 6510 spinning: the cassette
// buffer at $033C, which is RAM under every banking these tests use and
// is well clear of the $0400 video matrix, the $1000 character generator
// and sprite data, and the zero page the VIC tests set up.
const cpuParkAddr uint16 = 0x033C

// parkCPU leaves the 6510 executing a JMP to itself, and nothing else.
//
// StepCycle is a whole bus cycle, not just a VIC-II cycle: it clocks the
// singleton CPU and both CIAs as well. A test that drives the video logic
// by stepping cycles is therefore also running the 6510 - from wherever
// the previous test happened to leave the program counter, over RAM this
// test has just filled with character and sprite data. Sooner or later
// that lands on a byte which is not a legal opcode and the CPU panics, in
// a test that has nothing to do with the CPU; whether it does depends
// only on the order the suite ran in.
//
// A JMP to its own address is the smallest thing a 6510 can be given to
// do indefinitely: three cycles, three reads of the same three bytes and
// no writes at all, so the CPU can neither run off the end of the stub
// nor disturb what the test has set up. Interrupts are masked with it,
// because the parked CPU is only harmless for as long as it stays parked,
// and an IRQ would vector it into the KERNAL.
func parkCPU() {
	ram[cpuParkAddr] = 0x4C // JMP $033C
	ram[cpuParkAddr+1] = byte(cpuParkAddr & 0xFF)
	ram[cpuParkAddr+2] = byte(cpuParkAddr >> 8)
	cpu = CPU{}
	cpu.PC = cpuParkAddr
	cpu.SP = 0xFF
	cpu.regP |= P_INTERRUPT
}

// parkMachine is the setup for a test that drives a VIC-II of its own
// rather than the singleton: it snapshots the machine, cold-starts it and
// parks the CPU, so that stepping cycles clocks a machine in a state this
// test chose instead of one the previous test left behind. The CIAs are
// part of that - a timer left running by an earlier test is an interrupt
// arriving in the middle of this one, and CIA2 in particular can raise an
// NMI, which no amount of masking in the CPU would keep out.
func parkMachine(t *testing.T) {
	saveMachine(t)
	coldStart()
	parkCPU()
}

// newMachine builds a cold C64 and resets it, which is the order a front
// end uses when it has no disk to insert.
func newMachine(t *testing.T) *machine {
	saveMachine(t)
	coldStart()
	Reset()
	return &machine{t: t}
}

// run advances the machine by n CPU cycles. The VIC-II is what drives the
// CPU, and a bus cycle is the unit it steps in.
func (m *machine) run(cycles int) {
	for range cycles {
		vic.StepCycle()
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
	case '%':
		return true, Key5, true
	case '"':
		return true, Key2, true
	case '>':
		return true, KeyPeriod, true
	case '(':
		return true, Key8, true
	case ')':
		return true, Key9, true
	case '↑':
		return false, KeyUpArrow, true
	case '←':
		return false, KeyLeftArrow, true
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

// runUntil steps the machine until reached reports true, or until the
// budget is spent. Unlike waitForScreen it watches the machine itself
// rather than the screen, which is what a cartridge that never brings up
// a screen editor needs.
func (m *machine) runUntil(what string, budget int, reached func() bool) {
	m.t.Helper()
	for range budget {
		if reached() {
			return
		}
		vic.StepCycle()
	}
	m.t.Fatalf("did not reach %s; PC=$%04X", what, cpu.PC)
}

// waitForScreen runs the machine until want appears anywhere on the
// screen, or until the budget is spent. The failure dumps every non-blank
// row, because a test that was waiting on text is almost always easier to
// diagnose from what the machine actually printed instead.
func (m *machine) waitForScreen(want string) {
	m.t.Helper()
	const budget = 80_000_000
	for spent := 0; spent < budget; spent += 100_000 {
		if screenHas(want) {
			return
		}

		m.run(100_000)
	}
	var screen strings.Builder
	for row := range 25 {
		if line := screenLine(row); line != "" {
			screen.WriteString("\n")
			screen.WriteString(line)
		}
	}
	m.t.Fatalf("after %d cycles, screen does not contain %q; PC=$%04X screen:%s",
		budget, want, cpu.PC, screen.String())
}

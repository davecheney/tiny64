package tiny64

// The C64 keyboard is an 8x8 switch matrix wired between CIA1's two 8-bit
// ports. Each key is a simple momentary switch bridging one Port A line to
// one Port B line; there is no encoder chip and no scan-code hardware.
//
// The KERNAL scans it by configuring Port A as output (DDRA=$FF) and Port
// B as input (DDRB=$00), then writing a walking zero to $DC00 and reading
// $DC01 after each write. Everything is active low: a pressed key shorts
// its PA line to its PB line, so a PA line driven low drags the PB lines
// of any keys pressed on that line low too.
//
// The connection is a plain piece of wire, so it conducts both ways:
// software can equally drive Port B and read Port A, which some titles do
// to test for any keypress at all without walking all eight lines. Scan
// therefore models the matrix as a wired-AND in both directions. That is
// not only for the benefit of software that scans backwards: the reverse
// direction is also what produces the "ghosting" real hardware exhibits
// when three keys forming a rectangle in the matrix are held at once,
// since the phantom fourth key is only reachable by a path that runs back
// up a PA line and down again.
//
// Two keys are deliberately absent from the matrix, because they are
// absent on real hardware too:
//
//   - RESTORE is not scanned at all. It is wired through a monostable
//     directly to the CPU's NMI line, so it is modelled separately, by
//     Restore rather than by Press.
//   - SHIFT LOCK is a mechanical latch on the LEFT SHIFT contacts rather
//     than a separate switch, so it is modelled by holding KeyLShift.
//
// The joystick ports also share these two CIA1 ports (port 2 on PA, port
// 1 on PB), which is why moving a stick types garbage on a real machine.
// Scan takes the port values as arguments rather than reading them
// directly so that a joystick implementation can be composed in later
// without disturbing any of this.

// A Key identifies one position in the keyboard matrix, encoded as the
// Port A line (the "row", 0-7) in the high 3 bits and the Port B line (the
// "column", 0-7) in the low 3 bits.
type Key uint8

func matrixKey(row, col uint8) Key { return Key(row<<3 | col) }

// Row reports which Port A line this key is wired to.
func (k Key) Row() uint8 { return uint8(k) >> 3 }

// Col reports which Port B line this key is wired to.
func (k Key) Col() uint8 { return uint8(k) & 7 }

// The keyboard matrix, laid out by the Port A line that selects each group
// and the Port B line that senses it. This is the physical wiring of the
// machine, so the groupings look arbitrary: they follow the PCB, not the
// key caps.
const (
	// PA0
	KeyDelete      = Key(0<<3 | 0) // INST/DEL
	KeyReturn      = Key(0<<3 | 1)
	KeyCursorRight = Key(0<<3 | 2) // CRSR left/right; left needs SHIFT
	KeyF7          = Key(0<<3 | 3)
	KeyF1          = Key(0<<3 | 4)
	KeyF3          = Key(0<<3 | 5)
	KeyF5          = Key(0<<3 | 6)
	KeyCursorDown  = Key(0<<3 | 7) // CRSR up/down; up needs SHIFT

	// PA1
	Key3      = Key(1<<3 | 0)
	KeyW      = Key(1<<3 | 1)
	KeyA      = Key(1<<3 | 2)
	Key4      = Key(1<<3 | 3)
	KeyZ      = Key(1<<3 | 4)
	KeyS      = Key(1<<3 | 5)
	KeyE      = Key(1<<3 | 6)
	KeyLShift = Key(1<<3 | 7) // also SHIFT LOCK

	// PA2
	Key5 = Key(2<<3 | 0)
	KeyR = Key(2<<3 | 1)
	KeyD = Key(2<<3 | 2)
	Key6 = Key(2<<3 | 3)
	KeyC = Key(2<<3 | 4)
	KeyF = Key(2<<3 | 5)
	KeyT = Key(2<<3 | 6)
	KeyX = Key(2<<3 | 7)

	// PA3
	Key7 = Key(3<<3 | 0)
	KeyY = Key(3<<3 | 1)
	KeyG = Key(3<<3 | 2)
	Key8 = Key(3<<3 | 3)
	KeyB = Key(3<<3 | 4)
	KeyH = Key(3<<3 | 5)
	KeyU = Key(3<<3 | 6)
	KeyV = Key(3<<3 | 7)

	// PA4
	Key9 = Key(4<<3 | 0)
	KeyI = Key(4<<3 | 1)
	KeyJ = Key(4<<3 | 2)
	Key0 = Key(4<<3 | 3)
	KeyM = Key(4<<3 | 4)
	KeyK = Key(4<<3 | 5)
	KeyO = Key(4<<3 | 6)
	KeyN = Key(4<<3 | 7)

	// PA5
	KeyPlus   = Key(5<<3 | 0)
	KeyP      = Key(5<<3 | 1)
	KeyL      = Key(5<<3 | 2)
	KeyMinus  = Key(5<<3 | 3)
	KeyPeriod = Key(5<<3 | 4)
	KeyColon  = Key(5<<3 | 5)
	KeyAt     = Key(5<<3 | 6)
	KeyComma  = Key(5<<3 | 7)

	// PA6
	KeyPound     = Key(6<<3 | 0) // GBP
	KeyAsterisk  = Key(6<<3 | 1)
	KeySemicolon = Key(6<<3 | 2)
	KeyHome      = Key(6<<3 | 3) // CLR/HOME
	KeyRShift    = Key(6<<3 | 4)
	KeyEquals    = Key(6<<3 | 5)
	KeyUpArrow   = Key(6<<3 | 6)
	KeySlash     = Key(6<<3 | 7)

	// PA7
	Key1         = Key(7<<3 | 0)
	KeyLeftArrow = Key(7<<3 | 1)
	KeyCtrl      = Key(7<<3 | 2)
	Key2         = Key(7<<3 | 3)
	KeySpace     = Key(7<<3 | 4)
	KeyCommodore = Key(7<<3 | 5)
	KeyQ         = Key(7<<3 | 6)
	KeyRunStop   = Key(7<<3 | 7)
)

// Keyboard holds the pressed/released state of the key matrix. It has no
// clock of its own: the matrix is combinational logic, so it is resolved
// at the moment CIA1's ports are read. Scan cadence is whatever the
// running software chooses, which for the KERNAL is its ~60Hz CIA1 Timer
// A interrupt calling the scan routine at $EA87.
type Keyboard struct {
	// rows[pa] has bit pb set when the key at that intersection is held.
	rows [8]uint8
}

var keyboard Keyboard

// Keys returns the singleton keyboard matrix wired to CIA1.
func Keys() *Keyboard { return &keyboard }

// Press holds a key down. Pressing an already-held key does nothing, so
// callers may drive this from an edge- or level-triggered input source.
func (k *Keyboard) Press(key Key) { k.rows[key.Row()] |= 1 << key.Col() }

// Release lifts a key.
func (k *Keyboard) Release(key Key) { k.rows[key.Row()] &^= 1 << key.Col() }

// IsPressed reports whether a key is currently held.
func (k *Keyboard) IsPressed(key Key) bool {
	return k.rows[key.Row()]&(1<<key.Col()) != 0
}

// ReleaseAll lifts every key, as if the user let go of the keyboard. Front
// ends should call this when the window loses focus, otherwise a key held
// as focus is lost stays stuck down forever.
func (k *Keyboard) ReleaseAll() { k.rows = [8]uint8{} }

// Restore latches an NMI independently of the keyboard matrix and CIA2.
// Call it once per press edge, not repeatedly while the key is held.
// This event model omits pulse width and permits RESTORE while CIA2 holds
// NMI asserted, unlike the hardware's combined line.
func (k *Keyboard) Restore() {
	if k == &keyboard {
		cpu.nmiLatch = true
	}
}

// scan resolves the matrix given the electrical state CIA1 is driving onto
// Port A and Port B, returning the levels actually present on the pins.
//
// Each pressed key ties a PA line to a PB line, so the pins form a set of
// connected islands; any island containing a line held low reads low
// throughout. Both ports therefore have to be resolved together, and
// iteratively, because pulling one line low can complete a path that pulls
// another low in turn. Bits only ever fall from 1 to 0, so this converges
// in at most 16 passes.
func (k *Keyboard) scan(pa, pb uint8) (uint8, uint8) {
	for {
		before, beforeB := pa, pb
		for row := range k.rows {
			pressed := k.rows[row]
			if pressed == 0 {
				continue
			}
			if pa&(1<<row) == 0 {
				// This PA line is low, so drag every PB line
				// reachable through a pressed key down with it.
				pb &^= pressed
			}
			if pressed&^pb != 0 {
				// A PB line reachable from this row is low, so the
				// PA line is pulled low through the same switch.
				pa &^= 1 << row
			}
		}
		if pa == before && pb == beforeB {
			return pa, pb
		}
	}
}

// cia1ReadPRA and cia1ReadPRB construct CIA1's port read values. They have
// to be resolved as a pair, since the keyboard couples the two ports
// together: reading either one depends on what the other is driving.
func cia1ReadPRA() uint8 {
	pa, _ := keyboard.scan(effective(cia1.PRA, cia1.DDRA), effective(cia1.PRB, cia1.DDRB))
	return pa
}

func cia1ReadPRB() uint8 {
	_, pb := keyboard.scan(effective(cia1.PRA, cia1.DDRA), effective(cia1.PRB, cia1.DDRB))
	return pb
}

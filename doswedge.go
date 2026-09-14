package tiny64

import "sync"

const (
	// The wedge is an 8K cartridge: an EPROM on the expansion port's /ROML
	// chip-select, which the PLA maps at $8000-$9FFF.
	dosWedgeOrigin = 0x8000
	dosWedgeSize   = 0x2000

	// Two JMPs sit at fixed offsets just past the autostart signature. The
	// stub in RAM calls back into the cartridge through them, so they are
	// part of its interface and have to stay where they are however the
	// code behind them moves.
	dosWedgeReactivate = dosWedgeOrigin + 9
	dosWedgeEntry      = dosWedgeOrigin + 12

	// The cartridge's bank-control latch, in the expansion port's I/O1
	// window: writing bit 0 releases /EXROM and takes this ROM out of the
	// CPU's map, and writing it clear puts the ROM back.
	dosWedgeControl = 0xDE00
	dosWedgeBankIn  = 0x00
	dosWedgeBankOut = 0x01

	// Code cannot take the ROM it is executing from out of the map, so the
	// instructions that work the latch live in RAM. The resident ones go in
	// the cassette buffer: tiny64 models no datasette, and RAMTAS has both
	// pointed $B2/$B3 here and cleared the page by the time the cartridge's
	// startup code gets to it. SYS 828 lands on the first of them.
	dosWedgeStub     = 0x033C
	dosWedgeStubSize = 0x40

	// At reset there is nowhere else to put code at all: RAMTAS clears
	// $0002-$0101 and $0200-$03FF on its way past, which is the cassette
	// buffer and everything else low. The screen page is the one piece of
	// RAM it leaves alone - its memory walk puts back every byte it tests -
	// and CINT clears the screen a moment later in any case.
	dosWedgeBoot = 0x0400

	// The wedge's mutable state follows the stub through the rest of the
	// cassette buffer.
	dosWedgeWork        = dosWedgeStub + dosWedgeStubSize
	wedgeRestorePending = dosWedgeWork + 0  // $037C
	wedgeCurrentDevice  = dosWedgeWork + 1  // $037D
	wedgeDeviceParse    = dosWedgeWork + 2  // $037E
	wedgeDeviceDigit    = dosWedgeWork + 3  // $037F
	wedgeStringStart    = dosWedgeWork + 4  // $0380
	wedgeIOStatus       = dosWedgeWork + 5  // $0381
	wedgeSavedProgram   = dosWedgeWork + 6  // $0382-$0385, BASIC's $2B-$2E
	wedgeNameBuffer     = dosWedgeWork + 10 // $0386-$03D6, 80 characters and a terminator
	wedgeWorkEnd        = wedgeNameBuffer + 81

	// The cassette buffer ends at $03FB. If the workspace ever outgrows it
	// this conversion is negative and the package stops compiling.
	_ = uint(0x03FC - wedgeWorkEnd)

	// KERNAL entry points the cartridge's startup code uses. These four
	// have documented jump table entries, so they are stable across ROM
	// revisions.
	kernalCINT   = 0xFF81
	kernalIOINIT = 0xFF84
	kernalRAMTAS = 0xFF87
	kernalRESTOR = 0xFF8A

	// The KERNAL's NMI handler runs the same autostart signature check the
	// reset does, and jumps through $8002 when it matches, so a cartridge
	// owns NMI whether it wants to or not. This is the instruction right
	// after that jump - handing NMI straight back makes RESTORE behave as
	// it does with an empty expansion port. Unlike the four above it has no
	// jump table entry; TestDOSWedgeEntryPointsMatchROM pins it.
	kernalNMIResume = 0xFE5E

	// BASIC's cold start, $E394, is three subroutine calls and a jump. The
	// cartridge makes the same calls, so that it can put its own vector in
	// $0302 after $E453 has written BASIC's default one there. None of
	// these have jump table entries either.
	basicInitVectors  = 0xE453
	basicInitRAM      = 0xE3BF
	basicInitMessages = 0xE422
	basicColdTail     = 0xE386

	basicWarmStart     = 0xA483
	basicAfterReadLine = 0xA486
	basicReady         = 0xA474
	basicRelink        = 0xA533
	basicReadLine      = 0xA560
	petsciiUpArrow     = 0x5E
	petsciiLeftArrow   = 0x5F
)

// The image is the same every time, and assembling it costs more than
// keeping it.
var dosWedgeROM = sync.OnceValue(buildDOSWedge)

// EnableDOSWedge plugs an 8K DOS wedge cartridge into the expansion port.
//
// It takes effect at the next Reset, the way pushing a cartridge into a
// real machine does: during the reset sequence the KERNAL looks for the
// cartridge's CBM80 signature at $8004 and, finding it, hands over at
// $FCEC before any of the normal initialization has run. The cartridge
// performs that initialization itself, and then releases /EXROM and drops
// out of the CPU's map, leaving the 8K it was occupying to BASIC. It banks
// itself back in for the length of each direct-mode line, through a stub
// in the cassette buffer that BASIC's $0302 vector points at.
func EnableDOSWedge() {
	// /GAME floating, /EXROM asserted, a ROM chip on /ROML: an ordinary 8K
	// cartridge at $8000-$9FFF, with a bank-control latch at $DE00.
	bus.Insert(dosWedgeROM(), false, true, false, true)
	cartridge.Control = true
}

// DisableDOSWedge unplugs the cartridge. Like pulling one out of a real
// machine it takes effect at the next Reset; a wedge that is already
// running stays until then, or until @Q retires it. It empties the
// expansion port, whatever is in it.
func DisableDOSWedge() {
	bus.Remove()
}

// A fixup is an operand whose value is a label's address, filled in by
// finish once every label is known. Which part of the address, and how it
// is encoded, depends on the instruction that wanted it.
type wedgeFixupKind int

const (
	fixupWord   wedgeFixupKind = iota // two-byte absolute address
	fixupBranch                       // one-byte offset relative to the next instruction
	fixupLow                          // low byte of an address, as an immediate
	fixupHigh                         // high byte of an address, as an immediate
)

type wedgeFixup struct {
	at    int
	label string
	kind  wedgeFixupKind
}

type wedgeAssembler struct {
	origin uint16
	code   []byte
	labels map[string]uint16
	fixups []wedgeFixup
}

func newWedgeAssembler(origin uint16) *wedgeAssembler {
	return &wedgeAssembler{origin: origin, labels: make(map[string]uint16)}
}

func (a *wedgeAssembler) pc() uint16 {
	return a.origin + uint16(len(a.code))
}

func (a *wedgeAssembler) label(name string) {
	a.labels[name] = a.pc()
}

// at returns the address a label was defined at. Code in one image that
// has to reach into another - the cartridge installing the address of the
// RAM stub's hook, say - goes through this rather than through a fixup,
// since fixups only resolve within the image that emitted them.
func (a *wedgeAssembler) at(label string) uint16 {
	addr, ok := a.labels[label]
	if !ok {
		panic("unknown DOS wedge label: " + label)
	}
	return addr
}

// org asserts that the next byte emitted lands at addr. The cartridge
// header's jump table is an interface the RAM stub calls through, so the
// entries have to be where the stub expects them; this says so in the one
// place where a stray byte would move them.
func (a *wedgeAssembler) org(addr uint16) {
	if a.pc() != addr {
		panic("DOS wedge image is not at its expected origin")
	}
}

func (a *wedgeAssembler) emit(bytes ...byte) {
	a.code = append(a.code, bytes...)
}

func (a *wedgeAssembler) abs(op byte, addr uint16) {
	a.emit(op, byte(addr), byte(addr>>8))
}

// refWord emits a two-byte little-endian address slot for finish to fill
// in once the label is known. The cartridge header's reset and NMI vectors
// are bare addresses rather than instruction operands, so they need this
// rather than ref.
func (a *wedgeAssembler) refWord(label string) {
	a.emit(0, 0)
	a.fixups = append(a.fixups, wedgeFixup{at: len(a.code) - 2, label: label})
}

func (a *wedgeAssembler) ref(op byte, label string) {
	a.emit(op)
	a.refWord(label)
}

// refLow and refHigh emit an immediate operand carrying one byte of a
// label's address, for code that installs an address somewhere rather than
// jumping to it.
func (a *wedgeAssembler) refLow(op byte, label string) {
	a.emit(op, 0)
	a.fixups = append(a.fixups, wedgeFixup{at: len(a.code) - 1, label: label, kind: fixupLow})
}

func (a *wedgeAssembler) refHigh(op byte, label string) {
	a.emit(op, 0)
	a.fixups = append(a.fixups, wedgeFixup{at: len(a.code) - 1, label: label, kind: fixupHigh})
}

// pad fills the image out to size bytes with the value an unprogrammed
// EPROM cell reads as.
func (a *wedgeAssembler) pad(size int) {
	for len(a.code) < size {
		a.emit(0xFF)
	}
}

func (a *wedgeAssembler) branch(op byte, label string) {
	a.emit(op, 0)
	a.fixups = append(a.fixups, wedgeFixup{at: len(a.code) - 1, label: label, kind: fixupBranch})
}

func (a *wedgeAssembler) finish() []byte {
	for _, fixup := range a.fixups {
		target, ok := a.labels[fixup.label]
		if !ok {
			panic("unknown DOS wedge label: " + fixup.label)
		}
		switch fixup.kind {
		case fixupBranch:
			next := a.origin + uint16(fixup.at+1)
			offset := int(target) - int(next)
			if offset < -128 || offset > 127 {
				panic("DOS wedge branch out of range: " + fixup.label)
			}
			a.code[fixup.at] = byte(int8(offset))
		case fixupLow:
			a.code[fixup.at] = byte(target)
		case fixupHigh:
			a.code[fixup.at] = byte(target >> 8)
		default:
			a.code[fixup.at] = byte(target)
			a.code[fixup.at+1] = byte(target >> 8)
		}
	}
	return a.code
}

// buildDOSWedgeBoot assembles the throwaway half of the startup code: the
// part that has to run with the cartridge out of the map, and so cannot
// run from the cartridge. The KERNAL's RAMTAS walks up from $0400 writing
// a byte and reading it back, and stops where the read disagrees, which is
// what sets the top of BASIC's memory. Run it with the ROM banked out and
// the walk finds RAM all the way to $A000, so BASIC gets the 8K the
// cartridge is sitting in rather than stopping short of it.
//
// The cartridge copies this to the screen page and JSRs to it. The return
// address is pushed while the ROM is still mapped and the last thing the
// stub does before its RTS is put the ROM back, so control returns into
// cartridge code that is there to receive it.
func buildDOSWedgeBoot() []byte {
	a := newWedgeAssembler(dosWedgeBoot)
	a.emit(0xA9, dosWedgeBankOut)
	a.abs(0x8D, dosWedgeControl)
	a.abs(0x20, kernalRAMTAS)
	a.emit(0xA9, dosWedgeBankIn)
	a.abs(0x8D, dosWedgeControl)
	a.emit(0x60)
	return a.finish()
}

// buildDOSWedgeStub assembles the resident half: the few instructions that
// have to stay in RAM for the life of the session, because they are the
// ones that work the bank-control latch. Everything they do is bank the
// cartridge in, call into it through the fixed JMPs in its header, and
// bank it out again, so that the ROM is only in the CPU's map for as long
// as wedge code is actually running in it.
func buildDOSWedgeStub() *wedgeAssembler {
	a := newWedgeAssembler(dosWedgeStub)

	// SYS 828 - the first thing in the buffer, so that the address a user
	// types does not move when the code around it does.
	a.label("reactivate")
	a.emit(0xA9, dosWedgeBankIn)
	a.abs(0x8D, dosWedgeControl)
	a.abs(0x20, dosWedgeReactivate)
	a.emit(0xA9, dosWedgeBankOut)
	a.abs(0x8D, dosWedgeControl)
	a.emit(0x60)

	// BASIC's $0302 vector points here. The screen editor runs first, with
	// the cartridge still out of the map - reading a line of input needs
	// nothing from it - so the ROM is only in the CPU's map for as long as
	// the wedge is actually looking at what was typed.
	//
	// The handler in the cartridge ends in an RTS with the carry saying
	// where BASIC should go next: clear to interpret the line in the input
	// buffer, set for the READY. prompt. Deciding it there rather than
	// jumping from the cartridge is what lets the ROM be gone again before
	// BASIC runs a single byte of the line.
	a.label("hook")
	a.abs(0x20, basicReadLine) // JSR the screen editor, as $A483 does
	a.emit(0xA9, dosWedgeBankIn)
	a.abs(0x8D, dosWedgeControl)
	a.abs(0x20, dosWedgeEntry)
	a.emit(0xA9, dosWedgeBankOut) // LDA leaves the carry alone
	a.abs(0x8D, dosWedgeControl)
	a.branch(0xB0, "ready")
	a.emit(0xA2, 0xFF, 0xA0, 0x01) // LDX #$FF; LDY #1: CHRGET at $0200
	a.abs(0x4C, basicAfterReadLine)
	a.label("ready")
	a.abs(0x4C, basicReady)

	// The tail of the cartridge's cold start. BASIC has to reach $E386
	// with the ROM gone, since from there on the machine is BASIC's.
	a.label("coldTail")
	a.emit(0xA9, dosWedgeBankOut)
	a.abs(0x8D, dosWedgeControl)
	a.abs(0x4C, basicColdTail)

	a.finish()
	if len(a.code) > dosWedgeStubSize {
		panic("DOS wedge stub does not fit in front of its workspace")
	}
	return a
}

func buildDOSWedge() []byte {
	boot := buildDOSWedgeBoot()
	stub := buildDOSWedgeStub()

	a := newWedgeAssembler(dosWedgeOrigin)

	// The cartridge header the KERNAL looks for. $8004-$8008 spell CBM80 in
	// shifted PETSCII; finding them at reset makes the KERNAL jump through
	// $8000 before it has initialized anything, and finding them during an
	// NMI makes it jump through $8002.
	a.refWord("coldStart")
	a.refWord("nmiEntry")
	a.emit(0xC3, 0xC2, 0xCD, 0x38, 0x30)

	// The two entries the RAM stub calls through. Nothing else in the
	// cartridge is reachable from outside it.
	a.org(dosWedgeReactivate)
	a.ref(0x4C, "reactivate")
	a.org(dosWedgeEntry)
	a.ref(0x4C, "entry")

	// The KERNAL reaches here with the stack pointer, the interrupt
	// disable and the decimal flag set up, but nothing else: the reset
	// sequence jumps to a cartridge before it calls IOINIT, RAMTAS, RESTOR
	// or CINT, so the cartridge makes those calls itself and then does
	// BASIC's cold start the same way $E394 does - with its own vector
	// going into $0302 after $E453 has put BASIC's default one there.
	a.label("coldStart")
	a.emit(0x78, 0xD8)       // SEI; CLD
	a.emit(0xA2, 0xFF, 0x9A) // LDX #$FF; TXS
	a.abs(0x8E, 0xD016)      // STX $D016, as the KERNAL does at $FCEF
	a.abs(0x20, kernalIOINIT)

	// RAMTAS runs from the screen page with the cartridge banked out, so
	// that its memory walk finds RAM where this ROM is and hands BASIC the
	// whole 38911 bytes. It also clears $0002-$0101 and $0200-$03FF, which
	// is why the resident stub and the workspace are copied down after it
	// rather than before.
	a.emit(0xA2, byte(len(boot)-1)) // LDX #len(boot)-1
	a.label("copyBoot")
	a.ref(0xBD, "bootImage") // LDA bootImage,X
	a.abs(0x9D, dosWedgeBoot)
	a.emit(0xCA) // DEX
	a.branch(0x10, "copyBoot")
	a.abs(0x20, dosWedgeBoot)

	a.abs(0x20, kernalRESTOR)
	a.abs(0x20, kernalCINT)
	a.emit(0x58) // CLI

	a.abs(0x20, basicInitVectors) // $0300-$030B, including $0302 = $A483
	a.ref(0x20, "installHook")    // the wedge owns the prompt from here

	a.emit(0xA2, byte(len(stub.code)-1)) // LDX #len(stub)-1
	a.label("copyStub")
	a.ref(0xBD, "stubImage") // LDA stubImage,X
	a.abs(0x9D, dosWedgeStub)
	a.emit(0xCA) // DEX
	a.branch(0x10, "copyStub")

	// $E3BF writes just below the stack pointer, so it has to be called at
	// the same one-JSR depth $E394 calls it at: keep it here rather than
	// moving any of this into a subroutine.
	a.abs(0x20, basicInitRAM)
	a.abs(0x20, basicInitMessages) // sign-on banner and the free-memory line

	// The workspace is initialized after RAMTAS has finished zeroing the
	// page it lives in.
	a.emit(0xA9, 0x08)
	a.abs(0x8D, wedgeCurrentDevice)
	a.emit(0xA9, 0x00)
	a.abs(0x8D, wedgeRestorePending)
	a.ref(0x20, "printBanner")

	a.emit(0xA2, 0xFB, 0x9A) // LDX #$FB; TXS, as $E394 does
	// The stub banks the cartridge out and continues at $E386, which is
	// LDX #$80 followed by JMP ($0300): that reaches READY. through
	// BASIC's error vector rather than jumping straight at it, so a
	// program that has hooked $0300 still sees it.
	a.abs(0x4C, stub.at("coldTail"))

	// The KERNAL's NMI handler checks for a cartridge signature just as the
	// reset does, and jumps through $8002 when it finds one. Most of the
	// time the ROM is banked out and the handler sees RAM there instead,
	// but a RESTORE during the moment the wedge is looking at an input
	// line lands here. It has no interest in NMI, so hand it back at the
	// instruction the KERNAL would have run next.
	a.label("nmiEntry")
	a.abs(0x4C, kernalNMIResume)

	// The RAM stub calls in here once the screen editor has left a
	// direct-mode line in the input buffer, so this gets first look at it.
	// Every path out ends in an RTS with the carry set for READY. and
	// clear to let BASIC interpret what is in the buffer; the stub banks
	// the cartridge out and acts on it.
	a.label("entry")
	a.abs(0xAD, wedgeRestorePending)
	a.branch(0xF0, "scanLine")
	a.emit(0xA9, 0x00) // LDA #0
	a.abs(0x8D, wedgeRestorePending)
	a.ref(0x20, "restoreProgram")

	a.label("scanLine")
	a.emit(0xA0, 0x00) // LDY #0
	a.label("skipSpaces")
	a.abs(0xB9, 0x0200) // LDA $0200,Y
	a.emit(0xC9, ' ')   // CMP #' '
	a.branch(0xD0, "dispatch")
	a.emit(0xC8) // INY
	a.branch(0xD0, "skipSpaces")

	a.label("dispatch")
	a.emit(0xC9, '$') // CMP #'$'
	a.branch(0xD0, "notDirectory")
	a.ref(0x4C, "directory")
	a.label("notDirectory")
	a.emit(0xC9, '@') // CMP #'@'
	a.branch(0xF0, "command")
	a.emit(0xC9, '>') // CMP #'>'
	a.branch(0xF0, "command")
	a.emit(0xC9, '/') // CMP #'/'
	a.branch(0xD0, "notBasicLoad")
	a.ref(0x4C, "basicLoad")
	a.label("notBasicLoad")
	a.emit(0xC9, petsciiUpArrow) // CMP #up-arrow
	a.branch(0xD0, "notLoadRun")
	a.ref(0x4C, "loadRun")
	a.label("notLoadRun")
	a.emit(0xC9, '%') // CMP #'%'
	a.branch(0xD0, "notMachineLoad")
	a.ref(0x4C, "machineLoad")
	a.label("notMachineLoad")
	a.emit(0xC9, petsciiLeftArrow) // CMP #left-arrow
	a.branch(0xD0, "basic")
	a.ref(0x4C, "basicSave")
	a.label("basic")
	a.emit(0x18, 0x60) // CLC; RTS: BASIC interprets the line itself

	// @ and > open the command channel with the rest of the line as its
	// filename, then read and print the resulting status through KERNAL calls.
	// @# changes the current device number, @$ lists the directory, and @Q
	// deactivates the wedge until SYS 828 runs the resident reactivator.
	a.label("command")
	a.emit(0xC8)        // INY
	a.abs(0xB9, 0x0200) // LDA $0200,Y
	a.emit(0xC9, '#')   // CMP #'#'
	a.branch(0xD0, "notDeviceSelect")
	a.ref(0x4C, "selectDevice")
	a.label("notDeviceSelect")
	a.emit(0xC9, '$') // CMP #'$'
	a.branch(0xD0, "notCommandDirectory")
	a.ref(0x4C, "directory")
	a.label("notCommandDirectory")
	a.emit(0xC9, 'Q') // CMP #'Q'
	a.branch(0xD0, "openDriveCommand")
	a.ref(0x4C, "deactivate")
	a.label("openDriveCommand")
	a.emit(0x98, 0xAA) // TYA; TAX
	a.abs(0x8E, wedgeStringStart)
	a.label("commandLength")
	a.abs(0xBD, 0x0200) // LDA $0200,X
	a.branch(0xF0, "openCommand")
	a.emit(0xE8) // INX
	a.branch(0xD0, "commandLength")
	a.label("openCommand")
	a.emit(0x8A, 0x38) // TXA; SEC
	a.abs(0xED, wedgeStringStart)
	a.abs(0xAE, wedgeStringStart) // LDX stringStart
	a.emit(0xA0, 0x02)            // LDY #2
	a.abs(0x20, 0xFFBD)           // SETNAM
	a.emit(0xA9, 0x0F)            // LDA #15
	a.abs(0xAE, wedgeCurrentDevice)
	a.emit(0xA0, 0x0F)
	a.abs(0x20, 0xFFBA) // SETLFS 15,dev,15
	a.abs(0x20, 0xFFC0) // OPEN
	a.branch(0xB0, "ready")
	a.emit(0xA2, 0x0F)  // LDX #15
	a.abs(0x20, 0xFFC6) // CHKIN
	a.branch(0xB0, "closeCommand")
	a.label("readStatus")
	a.abs(0x20, 0xFFCF) // CHRIN
	a.emit(0x48)        // PHA
	a.abs(0x20, 0xFFB7) // READST
	a.abs(0x8D, wedgeIOStatus)
	a.emit(0x68)        // PLA
	a.abs(0x20, 0xFFD2) // CHROUT
	a.abs(0xAD, wedgeIOStatus)
	a.branch(0xF0, "readStatus")
	a.label("closeCommand")
	a.abs(0x20, 0xFFCC) // CLRCHN
	a.emit(0xA9, 0x0F)  // LDA #15
	a.abs(0x20, 0xFFC3) // CLOSE
	a.label("ready")
	a.emit(0x38, 0x60) // SEC; RTS: straight to the READY. prompt

	a.label("selectDevice")
	a.emit(0xC8) // INY, first digit
	a.ref(0x20, "readDecimalDevice")
	a.branch(0xB0, "storeDevice") // carry set: parsed a decimal device number
	a.ref(0x4C, "ready")
	a.label("storeDevice")
	a.abs(0x8D, wedgeCurrentDevice)
	a.ref(0x4C, "ready")

	a.label("deactivate")
	a.emit(0xA9, byte(basicWarmStart&0xFF))
	a.abs(0x8D, 0x0302)
	a.emit(0xA9, byte(basicWarmStart>>8))
	a.abs(0x8D, 0x0303)
	a.ref(0x4C, "ready")

	// /NAME becomes LOAD"NAME",dev, up-arrow NAME queues RUN in the KERNAL
	// keyboard buffer for the next prompt and then becomes LOAD"NAME",dev,
	// %NAME becomes LOAD"NAME",dev,1, and left-arrow NAME becomes
	// SAVE"NAME",dev. BASIC and KERNAL still perform the actual disk
	// operation, including normal messages and error paths.
	a.label("basicLoad")
	a.emit(0xC8) // INY
	a.abs(0xB9, 0x0200)
	a.branch(0xD0, "basicLoadName")
	a.ref(0x4C, "basic")
	a.label("basicLoadName")
	a.ref(0x20, "copyName")
	a.ref(0x4C, "writeLoadCommand")

	a.label("loadRun")
	a.emit(0xC8) // INY
	a.abs(0xB9, 0x0200)
	a.branch(0xD0, "loadRunName")
	a.ref(0x4C, "basic")
	a.label("loadRunName")
	a.ref(0x20, "copyName")
	a.ref(0x20, "queueRun")
	a.ref(0x4C, "writeLoadCommand")

	a.label("machineLoad")
	a.emit(0xC8) // INY
	a.abs(0xB9, 0x0200)
	a.branch(0xD0, "machineLoadName")
	a.ref(0x4C, "basic")
	a.label("machineLoadName")
	a.ref(0x20, "copyName")
	a.ref(0x4C, "writeMachineLoadCommand")

	a.label("basicSave")
	a.emit(0xC8) // INY
	a.abs(0xB9, 0x0200)
	a.branch(0xD0, "basicSaveName")
	a.ref(0x4C, "basic")
	a.label("basicSaveName")
	a.ref(0x20, "copyName")
	a.ref(0x4C, "writeSaveCommand")

	a.label("copyName")
	a.emit(0xA2, 0x00) // LDX #0
	a.label("copyNameByte")
	a.abs(0xB9, 0x0200) // LDA $0200,Y
	a.abs(0x9D, wedgeNameBuffer)
	a.branch(0xF0, "copyNameDone")
	a.emit(0xC8, 0xE8) // INY; INX
	a.emit(0xE0, 0x50) // CPX #80
	a.branch(0xD0, "copyNameByte")
	a.emit(0xA9, 0x00)
	a.abs(0x9D, wedgeNameBuffer) // force termination at the line limit
	a.label("copyNameDone")
	a.emit(0x60)

	a.label("writeLoadCommand")
	a.ref(0x20, "copyLoadPrefix")
	a.ref(0x20, "writeNameAndDevice")
	a.ref(0x4C, "finishBufferedBasic")

	a.label("writeMachineLoadCommand")
	a.ref(0x20, "copyLoadPrefix")
	a.ref(0x20, "writeNameAndDevice")
	a.emit(0xA9, ',')
	a.abs(0x99, 0x0205)
	a.emit(0xC8, 0xA9, '1')
	a.abs(0x99, 0x0205)
	a.emit(0xC8)
	a.ref(0x4C, "finishBufferedBasic")

	a.label("writeSaveCommand")
	a.ref(0x20, "copySavePrefix")
	a.ref(0x20, "writeNameAndDevice")
	a.ref(0x4C, "finishBufferedBasic")

	a.label("copyLoadPrefix")
	a.emit(0xA2, 0x00) // LDX #0
	a.label("copyLoadPrefixByte")
	a.ref(0xBD, "loadPrefix")
	a.abs(0x9D, 0x0200)
	a.emit(0xE8)
	a.emit(0xE0, 0x05) // CPX #5
	a.branch(0xD0, "copyLoadPrefixByte")
	a.emit(0x60)

	a.label("copySavePrefix")
	a.emit(0xA2, 0x00) // LDX #0
	a.label("copySavePrefixByte")
	a.ref(0xBD, "savePrefix")
	a.abs(0x9D, 0x0200)
	a.emit(0xE8)
	a.emit(0xE0, 0x05) // CPX #5
	a.branch(0xD0, "copySavePrefixByte")
	a.emit(0x60)

	a.label("writeNameAndDevice")
	a.emit(0xA0, 0x00) // LDY #0
	a.label("copyBufferedName")
	a.abs(0xB9, wedgeNameBuffer)
	a.branch(0xF0, "finishName")
	a.abs(0x99, 0x0205) // STA $0205,Y
	a.emit(0xC8)
	a.branch(0xD0, "copyBufferedName")
	a.label("finishName")
	a.emit(0xA9, '"')
	a.abs(0x99, 0x0205)
	a.emit(0xC8)
	a.ref(0x20, "writeDevice")
	a.emit(0x60)

	a.label("finishBufferedBasic")
	a.emit(0xA9, 0x00)
	a.abs(0x99, 0x0205)
	a.ref(0x4C, "basic")

	// $ loads the directory through KERNAL LOAD at its native $0401 address,
	// temporarily points BASIC at it, and feeds LIST to the normal parser.
	// The original program pointers are restored before the next input line.
	a.label("directory")
	a.ref(0x20, "saveProgram")
	a.emit(0x98, 0xAA) // TYA; TAX
	a.abs(0x8E, wedgeStringStart)
	a.label("directoryLength")
	a.abs(0xBD, 0x0200)
	a.branch(0xF0, "loadDirectory")
	a.emit(0xE8)
	a.branch(0xD0, "directoryLength")
	a.label("loadDirectory")
	a.emit(0x8A, 0x38) // TXA; SEC
	a.abs(0xED, wedgeStringStart)
	a.abs(0xAE, wedgeStringStart)
	a.emit(0xA0, 0x02)
	a.abs(0x20, 0xFFBD) // SETNAM
	a.emit(0xA9, 0x01)  // LDA #1
	a.abs(0xAE, wedgeCurrentDevice)
	a.emit(0xA0, 0x01)
	a.abs(0x20, 0xFFBA) // SETLFS 1,dev,1
	a.emit(0xA9, 0x00)
	a.abs(0x20, 0xFFD5) // LOAD
	a.branch(0xB0, "directoryFailed")
	a.emit(0x86, 0x2D, 0x84, 0x2E) // STX $2D; STY $2E
	a.emit(0xA9, 0x01, 0x85, 0x2B)
	a.emit(0xA9, 0x04, 0x85, 0x2C)
	a.abs(0x20, basicRelink)
	a.emit(0xA9, 0x01)
	a.abs(0x8D, wedgeRestorePending) // STA restorePending
	a.emit(0xA2, 0x00)
	a.label("copyList")
	a.ref(0xBD, "listCommand")
	a.abs(0x9D, 0x0200)
	a.emit(0xE8)
	a.emit(0xE0, 0x05)
	a.branch(0xD0, "copyList")
	a.ref(0x4C, "basic")
	a.label("directoryFailed")
	a.ref(0x20, "restoreProgram")
	a.ref(0x4C, "ready")

	a.label("saveProgram")
	for i := uint16(0); i < 4; i++ {
		a.abs(0xAD, 0x002B+i)
		a.abs(0x8D, wedgeSavedProgram+i)
	}
	a.emit(0x60)

	a.label("restoreProgram")
	for i := uint16(0); i < 4; i++ {
		a.abs(0xAD, wedgeSavedProgram+i)
		a.abs(0x8D, 0x002B+i)
	}
	a.emit(0x60)

	a.label("queueRun")
	a.emit(0xA9, 'R')
	a.abs(0x8D, 0x0277)
	a.emit(0xA9, 'U')
	a.abs(0x8D, 0x0278)
	a.emit(0xA9, 'N')
	a.abs(0x8D, 0x0279)
	a.emit(0xA9, 0x0D)
	a.abs(0x8D, 0x027A)
	a.emit(0xA9, 0x04)
	a.abs(0x8D, 0x00C6)
	a.emit(0x60)

	a.label("writeDevice")
	a.emit(0xA9, ',')
	a.abs(0x99, 0x0205)
	a.emit(0xC8)
	a.abs(0xAD, wedgeCurrentDevice)
	a.emit(0xC9, 0x0A) // CMP #10
	a.branch(0x90, "writeDeviceOnes")
	a.emit(0xA2, '0') // LDX #'0'
	a.label("writeDeviceTensLoop")
	a.emit(0xC9, 0x0A) // CMP #10
	a.branch(0x90, "writeDeviceTens")
	a.emit(0xE9, 0x0A) // SBC #10
	a.emit(0xE8)       // INX
	a.ref(0x4C, "writeDeviceTensLoop")
	a.label("writeDeviceTens")
	a.emit(0x48, 0x8A) // PHA; TXA
	a.abs(0x99, 0x0205)
	a.emit(0xC8, 0x68) // INY; PLA
	a.label("writeDeviceOnes")
	a.emit(0x18, 0x69, '0') // CLC; ADC #'0'
	a.abs(0x99, 0x0205)
	a.emit(0xC8, 0x60) // INY; RTS

	a.label("readDecimalDevice")
	a.abs(0xB9, 0x0200) // LDA $0200,Y
	a.emit(0xC9, '0')
	a.branch(0x90, "readDecimalFail")
	a.emit(0xC9, '9'+1)
	a.branch(0xB0, "readDecimalFail")
	a.emit(0x38, 0xE9, '0') // SEC; SBC #'0'
	a.abs(0x8D, wedgeDeviceParse)
	a.emit(0xC8)
	a.abs(0xB9, 0x0200) // LDA $0200,Y
	a.emit(0xC9, '0')
	a.branch(0x90, "readDecimalDone")
	a.emit(0xC9, '9'+1)
	a.branch(0xB0, "readDecimalDone")
	a.emit(0x38, 0xE9, '0') // SEC; SBC #'0'
	a.emit(0x48)            // PHA
	a.abs(0xAD, wedgeDeviceParse)
	a.emit(0x0A) // ASL A, *2
	a.abs(0x8D, wedgeDeviceDigit)
	a.emit(0x0A, 0x0A) // *8
	a.emit(0x18)
	a.abs(0x6D, wedgeDeviceDigit) // *10
	a.abs(0x8D, wedgeDeviceParse)
	a.emit(0x68, 0x18) // PLA; CLC
	a.abs(0x6D, wedgeDeviceParse)
	a.abs(0x8D, wedgeDeviceParse)
	a.label("readDecimalDone")
	a.abs(0xAD, wedgeDeviceParse)
	a.emit(0x38, 0x60) // SEC; RTS
	a.label("readDecimalFail")
	a.emit(0x18, 0x60) // CLC; RTS

	a.label("printBanner")
	a.emit(0xA2, 0x00)
	a.label("printBannerByte")
	a.ref(0xBD, "banner")
	a.branch(0xF0, "printBannerDone")
	a.abs(0x20, 0xFFD2)
	a.emit(0xE8)
	a.branch(0xD0, "printBannerByte")
	a.label("printBannerDone")
	a.emit(0x60)

	// Read-only tables. Everything the wedge writes lives in the cassette
	// buffer instead; see the workspace constants at the top of the file.
	a.label("loadPrefix")
	a.emit('L', 'O', 'A', 'D', '"')
	a.label("savePrefix")
	a.emit('S', 'A', 'V', 'E', '"')
	a.label("listCommand")
	a.emit('L', 'I', 'S', 'T', 0x00)
	// No carriage returns: the free-memory line printed by $E422 has
	// already left the cursor at the start of the next row, and READY.
	// begins with one of its own.
	a.label("banner")
	a.emit([]byte("DOS WEDGE ACTIVE")...)
	a.emit(0x00)

	// BASIC's direct-mode vector points at the stub in the cassette
	// buffer, not into the cartridge: by the time BASIC gets there the ROM
	// is out of the map, so there would be nothing at the other end of a
	// vector into it.
	a.label("installHook")
	a.emit(0xA9, byte(stub.at("hook")))
	a.abs(0x8D, 0x0302)
	a.emit(0xA9, byte(stub.at("hook")>>8))
	a.abs(0x8D, 0x0303)
	a.emit(0x60)

	// SYS 828 runs the stub's reactivator, which banks the cartridge in
	// and comes through the JMP in the header to here.
	a.label("reactivate")
	a.ref(0x20, "installHook")
	// Unlike the cold start this leaves the workspace alone, so the device
	// selected with @# survives @Q and a later SYS.
	a.ref(0x20, "printBanner")
	a.emit(0x60)

	// The two pieces of code that run from RAM, carried in the cartridge
	// as data because that is the only place they can be kept.
	a.label("bootImage")
	a.emit(boot...)
	a.label("stubImage")
	a.emit(stub.code...)

	if len(a.code) > dosWedgeSize {
		panic("DOS wedge does not fit in an 8K cartridge")
	}
	a.pad(dosWedgeSize)
	return a.finish()
}

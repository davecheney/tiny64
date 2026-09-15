package tiny64

import "sync"

const (
	// The wedge is an 8K cartridge: an EPROM on the expansion port's /ROML
	// chip-select, which the PLA maps at $8000-$9FFF.
	dosWedgeOrigin = 0x8000
	dosWedgeSize   = 0x2000

	// The wedge's mutable state lives in the cassette buffer. tiny64 models
	// no datasette, and RAMTAS has both pointed $B2/$B3 here and cleared
	// the page by the time the cartridge's own startup code gets to it.
	dosWedgeWork        = 0x033C
	wedgeRestorePending = dosWedgeWork + 0  // $033C
	wedgeCurrentDevice  = dosWedgeWork + 1  // $033D
	wedgeDeviceParse    = dosWedgeWork + 2  // $033E
	wedgeDeviceDigit    = dosWedgeWork + 3  // $033F
	wedgeStringStart    = dosWedgeWork + 4  // $0340
	wedgeIOStatus       = dosWedgeWork + 5  // $0341
	wedgeSavedProgram   = dosWedgeWork + 6  // $0342-$0345, BASIC's $2B-$2E
	wedgeNameBuffer     = dosWedgeWork + 10 // $0346-$0396, 80 characters and a terminator
	wedgeInterruptMask  = wedgeNameBuffer + 81
	wedgeCallA          = wedgeInterruptMask + 1
	wedgeRAMEntry       = wedgeCallA + 1
	wedgeRAMKill        = wedgeRAMEntry + 3
	wedgeRAMGate        = wedgeRAMKill + 6
	wedgeRAMCall        = wedgeRAMGate + 3
	wedgeWorkEnd        = wedgeRAMGate + 48

	_ = uint(0x03FC - wedgeWorkEnd)

	// KERNAL entry points the cartridge's startup code uses. These four
	// have documented jump table entries, so they are stable across ROM
	// revisions.
	kernalCINT   = 0xFF81
	kernalIOINIT = 0xFF84
	kernalRAMTAS = 0xFF87
	kernalRESTOR = 0xFF8A

	// Internal KERNAL NMI tails, pinned by TestDOSWedgeEntryPointsMatchROM.
	kernalNMIStopScan = 0xF6BC
	kernalNMIWarm     = 0xFE66
	kernalNMIReturn   = 0xFE72

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
// Mapping changes immediately; call Reset before running the machine.
// The KERNAL's CBM80 autostart boots firmware which hides ROML during the
// memory test and installs a small dispatcher in the cassette buffer.
// BASIC and loaded programs retain all their normal RAM.
func EnableDOSWedge() {
	// /GAME floating, /EXROM asserted, a ROM chip on /ROML: an ordinary 8K
	// cartridge at $8000-$9FFF.
	bus.Insert(dosWedgeROM(), false, true, false, true)
	cartridge.dosWedge = true
}

// DisableDOSWedge immediately empties the expansion port, whatever is in it.
// Call Reset before continuing: removal does not retire a live firmware
// hook or finish a cartridge instruction. Use @Q for a safe firmware exit.
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

func (a *wedgeAssembler) emit(bytes ...byte) {
	a.code = append(a.code, bytes...)
}

func (a *wedgeAssembler) abs(op byte, addr uint16) {
	a.emit(op, byte(addr), byte(addr>>8))
}

// service calls external code with ROML hidden and the prompt's interrupt
// state restored. The RAM gate preserves arguments and returned flags.
func (a *wedgeAssembler) service(addr uint16) {
	a.emit(0x08, 0x48) // PHP; PHA
	a.emit(0xA9, byte(addr))
	a.abs(0x8D, wedgeRAMCall+1)
	a.emit(0xA9, byte(addr>>8))
	a.abs(0x8D, wedgeRAMCall+2)
	a.emit(0x68, 0x28) // PLA; PLP
	a.abs(0x20, wedgeRAMGate)
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

func buildDOSWedge() []byte {
	a := newWedgeAssembler(dosWedgeOrigin)
	io := newWedgeAssembler(dosWedgeIO)
	r := newWedgeAssembler(wedgeRAMEntry)

	io.label("boot")
	io.emit(0x78, 0xD8, 0xA2, 0xFF, 0x9A) // SEI; CLD; LDX #$FF; TXS
	io.abs(0x8E, 0xD016)
	io.abs(0x20, kernalIOINIT)
	io.emit(0xA9, 0)
	io.abs(0x8D, dosWedgeLatch)
	io.abs(0x20, kernalRAMTAS)
	io.abs(0x20, kernalRESTOR)
	io.abs(0x20, kernalCINT)
	io.abs(0x20, basicInitVectors)
	// BASIC's RAM initialization must run at the cold start's one-JSR depth.
	io.abs(0x20, basicInitRAM)
	io.abs(0x20, basicInitMessages)
	io.emit(0xA9, dosWedgeMap)
	io.abs(0x8D, dosWedgeLatch)
	io.ref(0x4C, "coldFinish")

	io.label("prompt")
	io.emit(0xA9, 0)
	io.abs(0x8D, dosWedgeLatch)
	io.abs(0xAD, wedgeRestorePending)
	io.branch(0xF0, "readLine")
	io.emit(0xA9, 0)
	io.abs(0x8D, wedgeRestorePending)
	for i := uint16(0); i < 4; i++ {
		io.abs(0xAD, wedgeSavedProgram+i)
		io.abs(0x8D, 0x002B+i)
	}
	io.label("readLine")
	io.abs(0x20, basicReadLine)
	io.emit(0x08, 0x68, 0x29, P_INTERRUPT) // PHP; PLA; AND #I
	io.abs(0x8D, wedgeInterruptMask)
	io.emit(0x78, 0xA9, dosWedgeMap) // SEI
	io.abs(0x8D, dosWedgeLatch)
	io.ref(0x4C, "entry")

	for _, exit := range []struct {
		name string
		addr uint16
	}{
		{"ready", basicReady},
		{"basic", basicAfterReadLine},
		{"coldTail", basicColdTail},
		{"kill", wedgeRAMKill},
	} {
		io.label(exit.name)
		io.emit(0xA9, 0)
		io.abs(0x8D, dosWedgeLatch)
		io.abs(0xAD, wedgeInterruptMask)
		io.branch(0xD0, exit.name+"Masked")
		io.emit(0x58) // CLI only after ROML is hidden
		io.label(exit.name + "Masked")
		if exit.name == "basic" {
			io.emit(0xA2, 0xFF, 0xA0, 0x01)
		}
		if exit.name == "kill" {
			io.emit(0xA9, dosWedgeKill)
		}
		io.abs(0x4C, exit.addr)
	}

	// The KERNAL has already saved A/X/Y and checked CIA2 and CBM80.
	// A normal NMI returns with the interrupted mapping unchanged. STOP
	// abandons that stack, so hide ROML before the KERNAL's warm-start tail.
	io.label("nmi")
	io.abs(0x20, kernalNMIStopScan)
	io.abs(0x20, 0xFFE1)
	io.branch(0xD0, "nmiReturn")
	io.emit(0xA9, 0)
	io.abs(0x8D, dosWedgeLatch)
	io.abs(0x4C, kernalNMIWarm)
	io.label("nmiReturn")
	io.abs(0x4C, kernalNMIReturn)

	r.abs(0x4C, io.labels["prompt"])
	r.abs(0x8D, dosWedgeLatch)
	r.abs(0x4C, basicReady)
	r.ref(0x4C, "beforeCall")
	r.abs(0x20, 0xFFFF) // operand filled by the firmware before each call
	r.emit(0x08, 0x78)  // PHP; SEI
	r.abs(0x8D, wedgeCallA)
	r.emit(0x68, 0x09, P_INTERRUPT, 0x48) // set I in saved result flags
	r.emit(0xA9, dosWedgeMap)
	r.abs(0x8D, dosWedgeLatch)
	r.abs(0xAD, wedgeCallA)
	r.emit(0x28, 0x60) // PLP; RTS
	r.label("beforeCall")
	r.abs(0x8D, wedgeCallA)
	r.emit(0x08, 0x68, 0x29, 0xFF^P_INTERRUPT)
	r.abs(0x0D, wedgeInterruptMask)
	r.emit(0x48, 0xA9, 0)
	r.abs(0x8D, dosWedgeLatch)
	r.abs(0xAD, wedgeCallA)
	r.emit(0x28)
	r.abs(0x4C, wedgeRAMCall)
	resident := r.finish()
	if int(wedgeRAMEntry)+len(resident) != wedgeWorkEnd {
		panic("DOS wedge dispatcher size does not match workspace layout")
	}

	// The cartridge header the KERNAL looks for. $8004-$8008 spell CBM80 in
	// shifted PETSCII; finding them at reset makes the KERNAL jump through
	// $8000 before it has initialized anything, and finding them during an
	// NMI makes it jump through $8002.
	a.emit(byte(dosWedgeIO&0xFF), byte(dosWedgeIO>>8))
	a.emit(byte(io.labels["nmi"]), byte(io.labels["nmi"]>>8))
	a.emit(0xC3, 0xC2, 0xCD, 0x38, 0x30)

	// The I/O bootstrap has finished the normal KERNAL/BASIC initialization.
	// Only now can firmware install code in RAM without RAMTAS erasing it.
	a.label("coldFinish")
	a.emit(0xA2, byte(len(resident)-1))
	a.label("install")
	a.ref(0xBD, "resident")
	a.abs(0x9D, wedgeRAMEntry)
	a.emit(0xCA)
	a.branch(0x10, "install")
	a.emit(0xA9, byte(wedgeRAMEntry&0xFF))
	a.abs(0x8D, 0x0302)
	a.emit(0xA9, byte(wedgeRAMEntry>>8))
	a.abs(0x8D, 0x0303)

	// The workspace is initialized after RAMTAS has finished zeroing the
	// page it lives in.
	a.emit(0xA9, 0x08)
	a.abs(0x8D, wedgeCurrentDevice)
	a.emit(0xA9, 0x00)
	a.abs(0x8D, wedgeRestorePending)
	a.ref(0x20, "printBanner")

	a.emit(0xA2, 0xFB, 0x9A) // LDX #$FB; TXS, as $E394 does
	// $E386 is LDX #$80 followed by JMP ($0300), which reaches READY.
	// through BASIC's error vector rather than jumping straight at it, so
	// a program that has hooked $0300 still sees it.
	a.abs(0x4C, io.labels["coldTail"])

	// BASIC jumps through $0302 immediately before reading each direct-mode
	// line. The wrapper keeps that behavior, but gets first look at the line
	// after the screen editor returns.
	a.label("entry")
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
	a.abs(0x4C, io.labels["basic"])

	// @ and > open the command channel with the rest of the line as its
	// filename, then read and print the resulting status through KERNAL calls.
	// @# changes the current device number, @$ lists the directory, and @Q
	// deactivates the cartridge until hardware reset.
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
	a.service(0xFFBD)             // SETNAM
	a.emit(0xA9, 0x0F)            // LDA #15
	a.abs(0xAE, wedgeCurrentDevice)
	a.emit(0xA0, 0x0F)
	a.service(0xFFBA) // SETLFS 15,dev,15
	a.service(0xFFC0) // OPEN
	a.branch(0x90, "commandOpened")
	a.ref(0x4C, "ready")
	a.label("commandOpened")
	a.emit(0xA2, 0x0F) // LDX #15
	a.service(0xFFC6)  // CHKIN
	a.branch(0xB0, "closeCommand")
	a.label("readStatus")
	a.service(0xFFCF) // CHRIN
	a.emit(0x48)      // PHA
	a.service(0xFFB7) // READST
	a.abs(0x8D, wedgeIOStatus)
	a.emit(0x68)      // PLA
	a.service(0xFFD2) // CHROUT
	a.abs(0xAD, wedgeIOStatus)
	a.branch(0xF0, "readStatus")
	a.label("closeCommand")
	a.service(0xFFCC)  // CLRCHN
	a.emit(0xA9, 0x0F) // LDA #15
	a.service(0xFFC3)  // CLOSE
	a.label("ready")
	a.abs(0x4C, io.labels["ready"])

	a.label("selectDevice")
	a.emit(0xC8) // INY, first digit
	a.ref(0x20, "readDecimalDevice")
	a.branch(0xB0, "storeDevice") // carry set: parsed a decimal device number
	a.abs(0x4C, io.labels["ready"])
	a.label("storeDevice")
	a.abs(0x8D, wedgeCurrentDevice)
	a.abs(0x4C, io.labels["ready"])

	a.label("deactivate")
	a.emit(0xA9, byte(basicWarmStart&0xFF))
	a.abs(0x8D, 0x0302)
	a.emit(0xA9, byte(basicWarmStart>>8))
	a.abs(0x8D, 0x0303)
	a.abs(0x4C, io.labels["kill"])

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
	a.emit(0xA2, 0xFF, 0xA0, 0x01)
	a.abs(0x4C, io.labels["basic"])

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
	a.service(0xFFBD)  // SETNAM
	a.emit(0xA9, 0x01) // LDA #1
	a.abs(0xAE, wedgeCurrentDevice)
	a.emit(0xA0, 0x01)
	a.service(0xFFBA) // SETLFS 1,dev,1
	a.emit(0xA9, 0x00)
	a.service(0xFFD5) // LOAD
	a.branch(0xB0, "directoryFailed")
	a.emit(0x86, 0x2D, 0x84, 0x2E) // STX $2D; STY $2E
	a.emit(0xA9, 0x01, 0x85, 0x2B)
	a.emit(0xA9, 0x04, 0x85, 0x2C)
	a.service(basicRelink)
	a.emit(0xA9, 0x01)
	a.abs(0x8D, wedgeRestorePending) // STA restorePending
	a.emit(0xA2, 0x00)
	a.label("copyList")
	a.ref(0xBD, "listCommand")
	a.abs(0x9D, 0x0200)
	a.emit(0xE8)
	a.emit(0xE0, 0x05)
	a.branch(0xD0, "copyList")
	a.emit(0xA2, 0xFF, 0xA0, 0x01)
	a.abs(0x4C, io.labels["basic"])
	a.label("directoryFailed")
	a.ref(0x20, "restoreProgram")
	a.abs(0x4C, io.labels["ready"])

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
	a.service(0xFFD2)
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

	a.label("resident")
	a.emit(resident...)
	if len(a.code) > dosWedgeIOBank || len(io.code) > dosWedgeIOSize {
		panic("DOS wedge does not fit in an 8K cartridge")
	}
	for label, addr := range a.labels {
		io.labels[label] = addr
	}
	a.pad(dosWedgeIOBank)
	io.pad(dosWedgeIOSize)
	a.emit(io.finish()...)
	return a.finish()
}

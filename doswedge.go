package tiny64

const (
	dosWedgeOrigin     = 0xC000
	dosWedgeReactivate = 0xCC00
	basicWarmStart     = 0xA483
	basicAfterReadLine = 0xA486
	basicReady         = 0xA474
	basicRelink        = 0xA533
	basicReadLine      = 0xA560
	petsciiUpArrow     = 0x5E
	petsciiLeftArrow   = 0x5F
)

type dosWedgeState struct {
	enabled   bool
	installed bool
}

var dosWedge dosWedgeState

// EnableDOSWedge arranges for a small resident DOS wedge to be installed
// after BASIC initializes its RAM vectors. The wedge occupies $C000-$CFFF
// and recognizes commands entered at the BASIC prompt.
func EnableDOSWedge() {
	dosWedge.enabled = true
	dosWedge.installed = false
}

// DisableDOSWedge disables the DOS wedge and restores BASIC's standard warm
// start vector if the wedge is currently installed.
func DisableDOSWedge() {
	dosWedge.enabled = false
	if ram[0x0302] == uint8(dosWedgeOrigin&0xFF) && ram[0x0303] == uint8(dosWedgeOrigin>>8) {
		ram[0x0302] = uint8(basicWarmStart & 0xFF)
		ram[0x0303] = uint8(basicWarmStart >> 8)
	}
	dosWedge.installed = false
}

func resetDOSWedge() {
	dosWedge.installed = false
}

func tickDOSWedge() {
	if !dosWedge.enabled || dosWedge.installed {
		return
	}

	// BASIC installs this vector during its cold start. Waiting for the
	// standard value avoids racing the ROM's initialization and makes the
	// wedge work after both cold and warm machine resets.
	if ram[0x0302] != uint8(basicWarmStart&0xFF) || ram[0x0303] != uint8(basicWarmStart>>8) {
		return
	}

	program := buildDOSWedge()
	copy(ram[dosWedgeOrigin:], program)
	ram[0x0302] = uint8(dosWedgeOrigin & 0xFF)
	ram[0x0303] = uint8(dosWedgeOrigin >> 8)
	dosWedge.installed = true
}

type wedgeFixup struct {
	at     int
	label  string
	branch bool
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

func (a *wedgeAssembler) padTo(addr uint16) {
	for a.pc() < addr {
		a.emit(0x00)
	}
}

func (a *wedgeAssembler) branch(op byte, label string) {
	a.emit(op, 0)
	a.fixups = append(a.fixups, wedgeFixup{at: len(a.code) - 1, label: label, branch: true})
}

func (a *wedgeAssembler) finish() []byte {
	for _, fixup := range a.fixups {
		target, ok := a.labels[fixup.label]
		if !ok {
			panic("unknown DOS wedge label: " + fixup.label)
		}
		if fixup.branch {
			next := a.origin + uint16(fixup.at+1)
			offset := int(target) - int(next)
			if offset < -128 || offset > 127 {
				panic("DOS wedge branch out of range: " + fixup.label)
			}
			a.code[fixup.at] = byte(int8(offset))
			continue
		}
		a.code[fixup.at] = byte(target)
		a.code[fixup.at+1] = byte(target >> 8)
	}
	return a.code
}

func buildDOSWedge() []byte {
	a := newWedgeAssembler(dosWedgeOrigin)

	// BASIC jumps through $0302 immediately before reading each direct-mode
	// line. The wrapper keeps that behavior, but gets first look at the line
	// after the screen editor returns.
	a.label("entry")
	a.ref(0xAD, "bannerPending") // LDA bannerPending
	a.branch(0xF0, "afterBanner")
	a.emit(0xA9, 0x00)           // LDA #0
	a.ref(0x8D, "bannerPending") // STA bannerPending
	a.ref(0x20, "printBanner")
	a.label("afterBanner")
	a.ref(0xAD, "restorePending") // LDA restorePending
	a.branch(0xF0, "readLine")
	a.emit(0xA9, 0x00)            // LDA #0
	a.ref(0x8D, "restorePending") // STA restorePending
	a.ref(0x20, "restoreProgram")

	a.label("readLine")
	a.abs(0x20, basicReadLine) // JSR screen editor
	a.emit(0xA0, 0x00)         // LDY #0
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
	a.emit(0xA2, 0xFF) // LDX #$FF
	a.emit(0xA0, 0x01) // LDY #1
	a.abs(0x4C, basicAfterReadLine)

	// @ and > open the command channel with the rest of the line as its
	// filename, then read and print the resulting status through KERNAL calls.
	// @# changes the current device number, @$ lists the directory, and @Q
	// deactivates the wedge until SYS 52224 runs the resident reactivator.
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
	a.ref(0x8E, "stringStart")
	a.label("commandLength")
	a.abs(0xBD, 0x0200) // LDA $0200,X
	a.branch(0xF0, "openCommand")
	a.emit(0xE8) // INX
	a.branch(0xD0, "commandLength")
	a.label("openCommand")
	a.emit(0x8A, 0x38) // TXA; SEC
	a.ref(0xED, "stringStart")
	a.ref(0xAE, "stringStart") // LDX stringStart
	a.emit(0xA0, 0x02)         // LDY #2
	a.abs(0x20, 0xFFBD)        // SETNAM
	a.emit(0xA9, 0x0F)         // LDA #15
	a.ref(0xAE, "currentDevice")
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
	a.ref(0x8D, "ioStatus")
	a.emit(0x68)        // PLA
	a.abs(0x20, 0xFFD2) // CHROUT
	a.ref(0xAD, "ioStatus")
	a.branch(0xF0, "readStatus")
	a.label("closeCommand")
	a.abs(0x20, 0xFFCC) // CLRCHN
	a.emit(0xA9, 0x0F)  // LDA #15
	a.abs(0x20, 0xFFC3) // CLOSE
	a.label("ready")
	a.abs(0x4C, basicReady)

	a.label("selectDevice")
	a.emit(0xC8) // INY, first digit
	a.ref(0x20, "readDecimalDevice")
	a.branch(0xB0, "storeDevice") // carry set: parsed a decimal device number
	a.abs(0x4C, basicReady)
	a.label("storeDevice")
	a.ref(0x8D, "currentDevice")
	a.abs(0x4C, basicReady)

	a.label("deactivate")
	a.emit(0xA9, byte(basicWarmStart&0xFF))
	a.abs(0x8D, 0x0302)
	a.emit(0xA9, byte(basicWarmStart>>8))
	a.abs(0x8D, 0x0303)
	a.abs(0x4C, basicReady)

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
	a.ref(0x9D, "nameBuffer")
	a.branch(0xF0, "copyNameDone")
	a.emit(0xC8, 0xE8) // INY; INX
	a.emit(0xE0, 0x50) // CPX #80
	a.branch(0xD0, "copyNameByte")
	a.emit(0xA9, 0x00)
	a.ref(0x9D, "nameBuffer") // force termination at the line limit
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
	a.ref(0xB9, "nameBuffer")
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
	a.abs(0x4C, basicAfterReadLine)

	// $ loads the directory through KERNAL LOAD at its native $0401 address,
	// temporarily points BASIC at it, and feeds LIST to the normal parser.
	// The original program pointers are restored before the next input line.
	a.label("directory")
	a.ref(0x20, "saveProgram")
	a.emit(0x98, 0xAA) // TYA; TAX
	a.ref(0x8E, "stringStart")
	a.label("directoryLength")
	a.abs(0xBD, 0x0200)
	a.branch(0xF0, "loadDirectory")
	a.emit(0xE8)
	a.branch(0xD0, "directoryLength")
	a.label("loadDirectory")
	a.emit(0x8A, 0x38) // TXA; SEC
	a.ref(0xED, "stringStart")
	a.ref(0xAE, "stringStart")
	a.emit(0xA0, 0x02)
	a.abs(0x20, 0xFFBD) // SETNAM
	a.emit(0xA9, 0x01)  // LDA #1
	a.ref(0xAE, "currentDevice")
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
	a.ref(0x8D, "restorePending") // STA restorePending
	a.emit(0xA2, 0x00)
	a.label("copyList")
	a.ref(0xBD, "listCommand")
	a.abs(0x9D, 0x0200)
	a.emit(0xE8)
	a.emit(0xE0, 0x05)
	a.branch(0xD0, "copyList")
	a.emit(0xA2, 0xFF, 0xA0, 0x01)
	a.abs(0x4C, basicAfterReadLine)
	a.label("directoryFailed")
	a.ref(0x20, "restoreProgram")
	a.abs(0x4C, basicReady)

	a.label("saveProgram")
	for i := uint16(0); i < 4; i++ {
		a.abs(0xAD, 0x002B+i)
		a.ref(0x8D, "savedProgram"+string(rune('0'+i)))
	}
	a.emit(0x60)

	a.label("restoreProgram")
	for i := uint16(0); i < 4; i++ {
		a.ref(0xAD, "savedProgram"+string(rune('0'+i)))
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
	a.ref(0xAD, "currentDevice")
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
	a.ref(0x8D, "deviceParse")
	a.emit(0xC8)
	a.abs(0xB9, 0x0200) // LDA $0200,Y
	a.emit(0xC9, '0')
	a.branch(0x90, "readDecimalDone")
	a.emit(0xC9, '9'+1)
	a.branch(0xB0, "readDecimalDone")
	a.emit(0x38, 0xE9, '0') // SEC; SBC #'0'
	a.emit(0x48)            // PHA
	a.ref(0xAD, "deviceParse")
	a.emit(0x0A) // ASL A, *2
	a.ref(0x8D, "deviceDigit")
	a.emit(0x0A, 0x0A) // *8
	a.emit(0x18)
	a.ref(0x6D, "deviceDigit") // *10
	a.ref(0x8D, "deviceParse")
	a.emit(0x68, 0x18) // PLA; CLC
	a.ref(0x6D, "deviceParse")
	a.ref(0x8D, "deviceParse")
	a.label("readDecimalDone")
	a.ref(0xAD, "deviceParse")
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

	a.label("bannerPending")
	a.emit(0x01)
	a.label("restorePending")
	a.emit(0x00)
	a.label("currentDevice")
	a.emit(0x08)
	a.label("deviceParse")
	a.emit(0x00)
	a.label("deviceDigit")
	a.emit(0x00)
	a.label("stringStart")
	a.emit(0x00)
	a.label("ioStatus")
	a.emit(0x00)
	for i := range 4 {
		a.label("savedProgram" + string(rune('0'+i)))
		a.emit(0x00)
	}
	a.label("loadPrefix")
	a.emit('L', 'O', 'A', 'D', '"')
	a.label("savePrefix")
	a.emit('S', 'A', 'V', 'E', '"')
	a.label("listCommand")
	a.emit('L', 'I', 'S', 'T', 0x00)
	a.label("banner")
	a.emit('\r')
	a.emit([]byte("DOS WEDGE ACTIVE")...)
	a.emit('\r', 0x00)
	a.label("nameBuffer")
	a.emit(make([]byte, 81)...)

	a.padTo(dosWedgeReactivate)
	a.label("reactivate")
	a.emit(0xA9, byte(dosWedgeOrigin&0xFF))
	a.abs(0x8D, 0x0302)
	a.emit(0xA9, byte(dosWedgeOrigin>>8))
	a.abs(0x8D, 0x0303)
	a.emit(0xA9, 0x01)
	a.ref(0x8D, "bannerPending")
	a.emit(0x60)

	program := a.finish()
	if len(program) > 0x1000 {
		panic("DOS wedge exceeds $C000-$CFFF")
	}
	return program
}

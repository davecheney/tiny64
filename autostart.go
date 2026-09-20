package tiny64

// autostartBootFrames fast-forwards through the KERNAL's boot, which has
// nothing worth watching in it. The boot is deterministic - RAMTAS at
// $FD50 saves each byte it tests, writes its own two patterns over it,
// and puts the original back, so power-on noise cannot change how long
// the memory test runs - and it measures exactly 109 frames. This is an
// optimisation rather than the trigger: the prompt is confirmed
// afterwards, so overshooting a faster boot is harmless and
// undershooting a slower one costs a few more frames.
const autostartBootFrames = 109

// autostartCommand is fed to the machine as one string. The RUN rides in
// the KERNAL's type-ahead buffer behind the carriage return that starts
// the load, and the screen editor takes it at the prompt that follows:
// the editor's queue read at $E5B4 shifts the buffer down and decrements
// the count one character at a time, and nothing in BASIC's LOAD or the
// KERNAL's clears it, so the four bytes survive the whole load. That is
// what leaves the prelude with a single prompt to find.
const autostartCommand = `LOAD"*",8,1` + "\r" + "RUN\r"

// KEYD is the KERNAL's ten-character type-ahead buffer and NDX the count
// of characters waiting in it. The screen editor drains them exactly as
// it drains keys the user pressed.
const (
	kernalKeyBuffer = 0x0277
	kernalKeyCount  = 0x00C6
	kernalKeyMax    = 10
)

// Autostart runs the machine to the BASIC prompt and types LOAD"*",8,1
// and RUN, then returns. It is a prelude: call it after Reset and before
// the front end's own run loop, which is then the same loop a normal
// boot runs. Nothing is left hooked, armed or running.
//
// It happens once. A reset from inside the emulated machine is not
// noticed and does not repeat it, deliberately - there is no hook to
// notice with. To autostart something else, run the program again.
//
// This is not the cartridge autostart the KERNAL performs when it finds
// a CBM80 signature at $8004; nothing is plugged into the expansion
// port, no ROM is added, and the DOS wedge is unaffected either way.
//
// "*" is CBM DOS's first-file wildcard, which is why no filename is
// needed: MakeD64FromPRG writes the PRG as the only file on a fresh
// disk, and on a real D64 "*" matches the first directory entry.
func Autostart() {
	for range autostartBootFrames {
		vic.StepFrame()
	}

	// A cartridge in the expansion port pushes the prompt out by a frame,
	// so confirm rather than assume.
	for range 30 {
		if BASICReady() {
			break
		}
		vic.StepFrame()
	}

	Type(autostartCommand)
}

// Type feeds s to the machine through the KERNAL's own keyboard buffer,
// so the KERNAL, BASIC and the screen editor handle it exactly as they
// would characters a person typed: nothing is patched and no ROM is
// added. The buffer holds ten characters, so anything longer goes in as
// the editor drains it.
func Type(s string) {
	for len(s) > 0 {
		n := min(len(s), kernalKeyMax)
		for i := range n {
			ram[kernalKeyBuffer+i] = s[i]
		}

		// The count last. An interrupt landing between the two sees a
		// buffer that is either empty or complete, never half written;
		// the wedge cartridge's own 6502 queueRun orders it the same way.
		ram[kernalKeyCount] = byte(n)

		s = s[n:]

		// The editor takes all ten within a frame. The second is slack.
		vic.StepFrame()
		vic.StepFrame()
	}
}

// BASICReady reports whether READY. is on the screen, compared in screen
// codes rather than PETSCII because that is what the VIC-II reads.
func BASICReady() bool {
	for row := range 25 {
		s := 0x0400 + row*40
		if ram[s] == 0x12 && ram[s+1] == 0x05 && ram[s+2] == 0x01 &&
			ram[s+3] == 0x04 && ram[s+4] == 0x19 && ram[s+5] == 0x2E {
			return true
		}
	}
	return false
}

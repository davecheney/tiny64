package tiny64

import "github.com/davecheney/tiny64/rom"

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

// EnableDOSWedge plugs an 8K DOS wedge cartridge into the expansion port.
//
// Mapping changes immediately; call Reset before running the machine.
// The KERNAL's CBM80 autostart boots firmware which hides ROML during the
// memory test and installs a small dispatcher in the cassette buffer.
// BASIC and loaded programs retain all their normal RAM.
func EnableDOSWedge() {
	// /GAME floating, /EXROM asserted, a ROM chip on /ROML: an ordinary 8K
	// cartridge at $8000-$9FFF. The image never varies, so it is a fixed
	// asset rather than something assembled at run time; the assembler
	// that produced it is test-only code, kept honest by
	// TestDOSWedgeEmbeddedImageIsTheBuiltImage.
	bus.Insert(rom.DOSWedge, false, true, false, true)
	cartridge.dosWedge = true
}

// DisableDOSWedge immediately empties the expansion port, whatever is in it.
// Call Reset before continuing: removal does not retire a live firmware
// hook or finish a cartridge instruction. Use @Q for a safe firmware exit.
func DisableDOSWedge() {
	bus.Remove()
}

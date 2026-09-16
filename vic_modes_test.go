package tiny64

import "testing"

func TestVICGraphicsModeColors(t *testing.T) {
	v := &VICII{
		background0:        1,
		registers22To2E:    [0x0D]uint8{2, 3, 4},
		gdPending:          0x1B, // 00, 01, 10, 11
		videoBufferPending: 0x0D00 | 0xC1,
	}

	testMode := func(name string, control1, control2 uint8, want []byte) {
		t.Helper()
		v.control1, v.control2 = control1, control2
		v.loadGraphicsData()
		for i, color := range want {
			if got, _ := v.nextGraphicsColor(); got != color {
				t.Errorf("%s pixel %d = %d, want %d", name, i, got, color)
			}
		}
	}

	// The high Color RAM bit selects multicolor text. Clear it to prove
	// MCM alone still uses standard text for that character.
	v.videoBufferPending = 0x0500 | 0xC1
	testMode("standard text in MCM", 0, 0x10, []byte{1, 1, 1, 5, 5, 1, 5, 5})

	v.videoBufferPending = 0x0D00 | 0xC1
	testMode("multicolor text", 0, 0x10, []byte{1, 1, 2, 2, 3, 3, 5, 5})

	v.videoBufferPending = 0x00D6
	testMode("standard bitmap", 0x20, 0, []byte{6, 6, 6, 13, 13, 6, 13, 13})

	v.videoBufferPending = 0x0D06
	testMode("multicolor bitmap", 0x20, 0x10, []byte{1, 1, 0, 0, 6, 6, 13, 13})

	v.videoBufferPending = 0x0D00 | 0xC1
	testMode("ECM text", 0x40, 0, []byte{4, 4, 4, 13, 13, 4, 13, 13})
}

func TestVICGraphicsModeAddresses(t *testing.T) {
	saveMachine(t)
	cia2.PRA, cia2.DDRA = 3, 3 // VIC bank 0

	v := &VICII{RC: 3, VC: 12, memPointers: 0x08}
	v.videoMatrixColor[0] = 0x00C1

	ram[0x2063] = 0xA1
	v.control1, v.control2 = 0x20, 0
	v.cycleGAccess()
	if got := v.gdPending; got != 0xA1 {
		t.Fatalf("standard bitmap g-access = %#02x, want %#02x", got, 0xA1)
	}

	ram[0x200B] = 0xB2
	v.VC = 0
	v.VMLI = 0
	v.control1, v.control2 = 0x40, 0
	v.cycleGAccess()
	if got := v.gdPending; got != 0xB2 {
		t.Fatalf("ECM text g-access = %#02x, want %#02x", got, 0xB2)
	}
}

func TestVICBitmapIdleAccessUsesIdleData(t *testing.T) {
	saveMachine(t)
	cia2.PRA, cia2.DDRA = 3, 3 // VIC bank 0

	v := &VICII{
		idle:               true,
		VC:                 12,
		RC:                 3,
		memPointers:        0x08,
		control1:           0x20, // standard bitmap
		videoBufferPending: 0x00C1,
	}
	ram[0x2063] = 0xA5
	ram[0x3FFF] = 0x5A

	v.cycleGAccess()

	if got := v.gdPending; got != 0x5A {
		t.Fatalf("idle bitmap g-access = %#02x, want idle byte %#02x", got, 0x5A)
	}
	if v.videoBufferPending != 0 {
		t.Fatalf("idle bitmap video data = %#04x, want 0", v.videoBufferPending)
	}
	if v.VC != 12 || v.VMLI != 0 {
		t.Fatalf("idle bitmap g-access advanced counters to VC=%d VMLI=%d", v.VC, v.VMLI)
	}
}

func TestVICReloadDotMatchesPerDotRule(t *testing.T) {
	for start := uint16(0); start < DotsPerLine; start++ {
		for control := 0; control < 256; control++ {
			v := VICII{dot: start, control2: uint8(control)}
			// Preserve the old per-dot rule independently of graphicsReloadPhase.
			phase := uint16(control & 7)
			if control&0x10 != 0 && phase == 7 {
				phase = 0
			}
			want := uint16(0xFFFF)
			matches := 0
			got := v.reloadDot()
			for offset := uint16(1); offset <= DotsPerCycle; offset++ {
				// Reload is checked after incrementing, before dotclock7 wraps.
				dot := start + offset
				oldReload := dot/8 >= 6 && dot/8 <= 45 && dot&7 == phase
				if oldReload {
					want = dot
					matches++
				}
				if (dot == got) != oldReload {
					t.Fatalf("start=%d control2=%#02x dot=%d: reload=%d, per-dot rule=%v",
						start, control, dot, got, oldReload)
				}
			}
			if matches > 1 || got != want {
				t.Fatalf("start=%d control2=%#02x: reload=%d, want %d (%d matches)",
					start, control, got, want, matches)
			}
			if got != noReloadDot && (got < 48 || got >= 368 || got <= start || got > start+DotsPerCycle) {
				t.Fatalf("start=%d control2=%#02x: reload=%d outside cell/cycle bounds",
					start, control, got)
			}
		}
	}
}

func TestVICXScrollDelaysGraphicsReload(t *testing.T) {
	// Dot 48 is the boundary of the first character cell of the display
	// window, and where an unscrolled sequencer takes up its g-access
	// result. reloadDot is asked at the start of the cycle that advances
	// onto the dot in question, so dot 41-48's cycle begins at dot 40.
	unscrolled := &VICII{}
	unscrolled.dot = 40
	if got := unscrolled.reloadDot(); got != 48 {
		t.Fatalf("unscrolled reload dot = %d, want the cell boundary at 48", got)
	}

	// XSCROLL=3 moves the reload three dots into the cell, to 51.
	v := &VICII{control2: 3, gdPending: 0xFF, videoBufferPending: 0x0100}
	v.dot = 40
	if got := v.reloadDot(); got == 48 {
		t.Fatal("sequencer reloaded on the cell boundary with XSCROLL=3")
	}
	v.dot = 48
	if got := v.reloadDot(); got != 51 {
		t.Fatalf("reload dot = %d with XSCROLL=3, want 51", got)
	}

	// And the data is actually taken up there. dotclock2 is the phase
	// that advances the beam onto dot 51.
	v.dot = 50
	v.dotclock2(51)
	// The sequencer holds two bits per dot, so the pending $FF is 0x5555
	// once widened - see expandGraphicsData.
	if v.gdSequencer != 0x5555 || v.videoBuffer != 0x0100 {
		t.Fatalf("sequencer=%#04x buffer=%#04x at dot %d, want pending graphics data",
			v.gdSequencer, v.videoBuffer, v.dot)
	}
}

func TestVICMulticolorXScrollSevenReloadsAtPairBoundary(t *testing.T) {
	// Multicolor pixels span two dot clocks, so XSCROLL=7 completes its
	// pair at phase 7 and reloads on the following cell boundary at dot
	// 56 rather than at 55.
	v := &VICII{
		control2:           0x17,
		gdPending:          0xFF,
		videoBufferPending: 0x0100,
	}
	v.dot = 47
	if got := v.reloadDot(); got == 55 {
		t.Fatal("sequencer reloaded at dot 55 in multicolor mode with XSCROLL=7")
	}
	v.dot = 48
	if got := v.reloadDot(); got != 56 {
		t.Fatalf("multicolor XSCROLL=7 reload dot = %d, want 56", got)
	}

	// dotclock7 is the phase that advances the beam onto dot 56.
	v.dot = 55
	v.dotclock7(56)
	// $FF widened two bits to the dot. This character is not multicolor
	// itself - bit 11 of the video buffer is clear - so it widens as a
	// standard one even though MCM is set.
	if v.gdSequencer != 0x5555 || v.videoBuffer != 0x0100 {
		t.Fatalf("sequencer=%#04x buffer=%#04x at dot %d, want pending graphics data",
			v.gdSequencer, v.videoBuffer, v.dot)
	}

	// At standard resolution the same XSCROLL reloads at 55, one dot
	// earlier, because there is no pair to finish.
	v = &VICII{control2: 7, gdPending: 0xA5}
	v.dot = 48
	if got := v.reloadDot(); got != 55 {
		t.Fatalf("standard-resolution XSCROLL=7 reload dot = %d, want 55", got)
	}
	v.dot = 54
	v.dotclock6(55)
	if v.gdSequencer != 0x4411 { // $A5 widened two bits to the dot
		t.Fatal("standard-resolution XSCROLL=7 did not reload at dot 55")
	}
}

func TestVICXScrollWriteReloadsAtCurrentDot(t *testing.T) {
	v := &VICII{
		dot:                48,
		control2:           7,
		gdPending:          0xA5,
		videoBufferPending: 0x0D06,
	}

	v.WriteRegister(0xD016, 0)

	if v.gdSequencer != 0x4411 || v.videoBuffer != 0x0D06 { // $A5 widened
		t.Fatalf("sequencer=%#04x buffer=%#04x after same-dot XSCROLL write, want pending graphics data",
			v.gdSequencer, v.videoBuffer)
	}
}

func TestVICSideBorderOpen40To38Trick(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.rasterLine = 100
	v.control1 = 0x1B // DEN=1, RSEL=1, YSCROLL=3
	v.control2 = 0x08 // CSEL=1 (40 columns)
	// Raster 100 is inside the display window, so the vertical border
	// flip-flop has already been cleared by the top comparison. Reset()
	// seeds it set because raster 0 is in the upper border.
	v.verticalBorder = false
	v.syncLineVisibility()

	// Advance beam to the 38-column right comparison in horizontal blanking.
	// The left comparison at dot 48
	// clears the main border flip-flop on the way.
	// In 40-column mode, this comparison does not latch mainBorder.
	for v.dot != rightComp38 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d in 40-column mode, want false", rightComp38)
	}

	// Switch CSEL to 0 after the 38-column comparison and before the
	// 40-column comparison. The two comparisons are one bus cycle apart
	// and a CPU write completes at the end of a cycle, so dot 408 is
	// where the write lands - after dotclock7 has run that dot's
	// comparison. sampleSideBorderAtWrite sees the coincidence but takes
	// no action on it: the 38-column path there needs rightBorderAt to be
	// rightEdge38, and 40-column mode set it to rightEdge40 back at dot
	// 368.
	v.WriteRegister(0xD016, 0x00) // CSEL=0

	// Since CSEL is now 0, the 40-column comparison is bypassed.
	for v.dot != rightComp40 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d with CSEL=0 trick, want false (side border opened)", rightComp40)
	}

	// In 40-column mode without the trick, its comparison latches mainBorder.
	vNormal := &VICII{}
	vNormal.Reset()
	vNormal.rasterLine = 100
	vNormal.control1 = 0x1B
	vNormal.control2 = 0x08 // CSEL=1
	vNormal.verticalBorder = false
	vNormal.syncLineVisibility()

	for vNormal.dot != rightComp40 {
		vNormal.StepCycle()
	}
	if !vNormal.mainBorder {
		t.Fatalf("mainBorder=false at dot %d in normal 40-column mode, want true", rightComp40)
	}
}

func TestVICSideBorderOpen38To40Trick(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.rasterLine = 100
	v.control1 = 0x1B // DEN=1, RSEL=1, YSCROLL=3
	v.control2 = 0x00 // CSEL=0 (38 columns)
	// Inside the display window the vertical border flip-flop is clear.
	v.verticalBorder = false
	v.syncLineVisibility()

	// In 38-column mode, switch to 40-column before its right comparison.
	v.WriteRegister(0xD016, 0x08) // CSEL=1 (40 cols)

	// Since CSEL=1, the 38-column comparison will not trigger mainBorder.
	for v.dot != rightComp38 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d with CSEL=1, want false", rightComp38)
	}

	// Switch back to CSEL=0 before the 40-column comparison - the write
	// lands on dot 408, the last dot of its cycle, which is after that
	// dot's comparison has run and is inert for the same reason as in
	// TestVICSideBorderOpen40To38Trick.
	v.WriteRegister(0xD016, 0x00) // CSEL=0 (38 cols)

	// Since CSEL=0, the 40-column comparison will not trigger it either.
	for v.dot != rightComp40 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d with CSEL=0, want false (side border opened)", rightComp40)
	}
}

func TestVICSideBorderWriteAtRightCompareDot(t *testing.T) {
	v := &VICII{
		dot:            rightComp38,
		rasterLine:     100,
		control2:       0,
		mainBorder:     true,
		rightBorderAt:  rightEdge38,
		lineDrawable:   true,
		verticalBorder: false,
	}

	v.WriteRegister(0xD016, csel)

	if v.mainBorder {
		t.Fatal("mainBorder=true after same-dot CSEL write bypassed right comparison")
	}
	if v.rightBorderAt != 0 {
		t.Fatalf("rightBorderAt=%d after bypassed comparison, want 0", v.rightBorderAt)
	}
}

func TestVICSideBorderRMWWritesSkipRightComparisons(t *testing.T) {
	v := &VICII{
		dot:            rightComp38,
		rasterLine:     100,
		control2:       0,
		mainBorder:     true,
		rightBorderAt:  rightEdge38,
		lineDrawable:   true,
		verticalBorder: false,
	}

	v.WriteRegister(0xD016, csel)
	if v.mainBorder {
		t.Fatalf("mainBorder=true after write at dot %d bypassed 38-column comparison", rightComp38)
	}

	v.dot = rightComp40
	v.WriteRegister(0xD016, 0)
	if v.mainBorder {
		t.Fatalf("mainBorder=true after write at dot %d bypassed 40-column comparison", rightComp40)
	}
	if v.rightBorderAt != 0 {
		t.Fatalf("rightBorderAt=%d after both comparisons were bypassed, want 0", v.rightBorderAt)
	}
	if !v.rightBorderOpen {
		t.Fatal("right border was not marked open after both comparisons were bypassed")
	}
}

func TestVICSideBorderWriteAtCompareDot(t *testing.T) {
	v := &VICII{}
	v.Reset()
	v.rasterLine = 100
	v.control1 = 0x1B
	v.control2 = 0x00 // CSEL=0
	v.verticalBorder = false
	v.syncLineVisibility()
	v.dot = leftComp40 - 1

	// dotclock7 is the phase that owns the 40-column left comparison, so
	// run just that one rather than a whole cycle around it.
	v.dotclock7(v.reloadDot()) // reaches the 40-column left compare with CSEL still clear
	if !v.mainBorder {
		t.Fatal("mainBorder=false before same-dot register write")
	}

	v.WriteRegister(0xD016, 0x08)
	if v.mainBorder {
		t.Fatal("mainBorder=true after same-dot CSEL write")
	}
}

func TestVICVerticalBorderOpenTopBottomTricks(t *testing.T) {
	// Rule 3: Top border compare ($33 for RSEL=1) clears verticalBorder if DEN=1
	v := &VICII{}
	v.Reset()
	v.control1 = 0x1B // DEN=1, RSEL=1 ($33)
	v.verticalBorder = true
	v.rasterLine = 0x33
	v.cycleBorderComp()
	if v.verticalBorder {
		t.Errorf("verticalBorder=true at raster $33 with DEN=1, want false")
	}

	// Rule 3: If DEN=0 at line $33, verticalBorder is not cleared
	v.Reset()
	v.control1 = 0x0B // DEN=0, RSEL=1 ($33)
	v.verticalBorder = true
	v.rasterLine = 0x33
	v.cycleBorderComp()
	if !v.verticalBorder {
		t.Errorf("verticalBorder=false at raster $33 with DEN=0, want true")
	}

	// Rule 2: Bottom border compare ($FA for RSEL=1) sets verticalBorder unconditionally
	v.Reset()
	v.control1 = 0x1B // DEN=1, RSEL=1 ($FA)
	v.verticalBorder = false
	v.rasterLine = 0xFB // bottomComp[1]
	v.cycleBorderComp()
	if !v.verticalBorder {
		t.Errorf("verticalBorder=false at bottom border line $FB, want true")
	}
}

// TestGraphicsPaletteMatchesPerDotDecode holds refreshGraphicsPalette to
// the decode nextGraphicsColor used to perform per dot, across every
// input that decode could branch on: all eight graphics modes including
// the invalid ones, multicolor either way, and every value the latched
// g-access data can take.
//
// The palette is a pure function of those inputs, so an exhaustive
// comparison against the old code is both possible and cheap, and it is
// worth having: several of these arms - the invalid modes, ECM's choice
// of background register - are reached by no other test, and a wrong
// colour in one of them would show up as nothing more than an odd pixel
// in a mode nobody runs.
func TestGraphicsPaletteMatchesPerDotDecode(t *testing.T) {
	// perDotDecode is what nextGraphicsColor did for a sequencer value of
	// index, before the mode branching was lifted out of the dot path.
	perDotDecode := func(v *VICII, index uint8) (byte, bool) {
		if !v.multicolor {
			if index == 0 {
				if v.graphicsMode == modeECMText {
					return v.backgroundColor(uint8(v.videoBuffer>>6) & 0x03), false
				}
				if v.graphicsMode == modeStandardBitmap {
					return byte(v.videoBuffer) & 0x0F, false
				}
				if v.graphicsMode > modeECMText {
					return 0, false
				}
				return v.background0, false
			}
			switch v.graphicsMode {
			case modeStandardText, modeMulticolorText, modeECMText:
				return byte(v.videoBuffer>>8) & 0x0F, true
			case modeStandardBitmap:
				return byte(v.videoBuffer>>4) & 0x0F, true
			default:
				return 0, false
			}
		}
		switch v.graphicsMode {
		case modeMulticolorText:
			switch index {
			case 0:
				return v.background0, false
			case 1:
				return v.backgroundColor(1), false
			case 2:
				return v.backgroundColor(2), false
			default:
				return byte(v.videoBuffer>>8) & 0x07, true
			}
		case modeMulticolorBitmap:
			switch index {
			case 0:
				return v.background0, false
			case 1:
				return byte(v.videoBuffer>>4) & 0x0F, true
			case 2:
				return byte(v.videoBuffer) & 0x0F, true
			default:
				return byte(v.videoBuffer>>8) & 0x0F, true
			}
		default:
			return 0, false
		}
	}

	v := &VICII{}
	// Distinct background colours, so swapping two of the four registers
	// cannot pass unnoticed.
	v.background0 = 0x01
	v.registers22To2E[0] = 0x02 // $D022
	v.registers22To2E[1] = 0x03 // $D023
	v.registers22To2E[2] = 0x04 // $D024

	for mode := uint8(0); mode < 8; mode++ {
		for _, multicolor := range []bool{false, true} {
			// Only the first two sequencer values are reachable in the
			// standard modes; the multicolor modes shift out all four.
			indices := uint8(2)
			if multicolor {
				indices = 4
			}
			for data := 0; data < 1<<16; data++ {
				v.graphicsMode, v.multicolor = mode, multicolor
				v.videoBuffer = uint16(data)
				v.refreshGraphicsPalette()

				for index := uint8(0); index < indices; index++ {
					wantColor, wantForeground := perDotDecode(v, index)
					gotColor := v.gdColor[index]
					gotForeground := v.gdForeground&(1<<index) != 0
					if gotColor != wantColor || gotForeground != wantForeground {
						t.Fatalf("mode %d multicolor=%v data=%#04x index %d: palette says colour %#02x foreground=%v, per-dot decode says %#02x foreground=%v",
							mode, multicolor, data, index, gotColor, gotForeground, wantColor, wantForeground)
					}
				}
			}
		}
	}
}

// TestGraphicsDataExpansionMatchesPerDotShift holds expandGraphicsData
// to the shifting nextGraphicsColor used to perform per dot, across every
// g-access byte and both pixel widths.
//
// The mode test moved out of the dot path and into the reload, which is
// only sound if the widened register shifts out the same sequence of
// palette indices the 8-bit one did - including past the eighth dot,
// where the register has emptied and every further dot has to read as
// index 0. Ten dots, so two of them land there.
func TestGraphicsDataExpansionMatchesPerDotShift(t *testing.T) {
	// perDotShift is what nextGraphicsColor did for one dot, before the
	// pixel width was baked into the register.
	perDotShift := func(seq *uint8, half *bool, multicolor bool) uint8 {
		if !multicolor {
			index := *seq >> 7
			*seq <<= 1
			return index
		}
		index := *seq >> 6
		if *half {
			*seq <<= 2
		}
		*half = !*half
		return index
	}

	v := &VICII{}
	for _, multicolor := range []bool{false, true} {
		for data := 0; data < 1<<8; data++ {
			seq, half := uint8(data), false
			v.gdSequencer = expandGraphicsData(uint8(data), multicolor)
			for dot := range 10 {
				want := perDotShift(&seq, &half, multicolor)
				// gdColor is the identity here, so the colour the
				// sequencer reports is the index it shifted out.
				v.gdColor = [4]uint8{0, 1, 2, 3}
				got, _ := v.nextGraphicsColor()
				if got != want {
					t.Fatalf("data=%#02x multicolor=%v dot %d: widened register shifts out index %d, per-dot shift gives %d",
						data, multicolor, dot, got, want)
				}
			}
		}
	}
}

// TestBackgroundWriteReachesTheNextDot pins the other half of the
// palette's contract. Lifting the decode out of the dot path means the
// colours are now derived ahead of the dots that use them, so a mid-line
// write to a background register has to be picked up by the cycle that
// follows it - that is what the raster tricks changing $D021 down the
// screen depend on.
func TestBackgroundWriteReachesTheNextDot(t *testing.T) {
	saveMachine(t)
	newMachine(t)
	iecBus = nil

	vic.rasterLine = 100
	vic.dot = 48
	vic.control1 = 0x1B // DEN=1, RSEL=1, YSCROLL=3
	vic.control2 = 0x08
	vic.syncLineVisibility()
	// Reset seeds the border flip-flops set, because raster 0 is in the
	// upper border; raster 100 is inside the display window.
	vic.mainBorder, vic.verticalBorder = false, false
	vic.background0 = 6
	vic.gdPending, vic.videoBufferPending = 0x00, 0x0100
	vic.loadGraphicsData() // sequencer all background

	vic.StepCycle()
	if !frameBufferPixelIs(52, 100, 6) {
		t.Fatal("dot 52 was not painted with the background colour in force")
	}

	vic.WriteRegister(0xD021, 3)
	vic.StepCycle()
	if !frameBufferPixelIs(60, 100, 3) {
		t.Fatal("a background write did not reach the dots of the following cycle")
	}
}

// TestGraphicsPaletteStaysConsistentAcrossAFrame is the guard on the
// palette's invalidation rule. The colours are derived ahead of the dots
// that use them and rebuilt only at the points where their inputs can
// change, so a new writer of the graphics mode, the latched g-access data
// or any of the four background registers that does not rebuild would
// leave the sequencer painting with stale colours - and would show up as
// nothing more than a wrong colour somewhere down the frame.
//
// So drive a full frame of a live display with sprites and a program
// writing registers, and after every bus cycle check the cached palette
// still equals one built from the state as it stands. Anything that
// changes an input without rebuilding fails here on the cycle it happens.
func TestGraphicsPaletteStaysConsistentAcrossAFrame(t *testing.T) {
	saveMachine(t)
	Reset()
	loadSpriteProgram()

	// A program that walks values through all four background registers
	// and through $D016, so the sweep below sees the palette's inputs
	// change under it rather than sitting still. It leaves $D011 alone:
	// writing YSCROLL mid-frame manufactures extra Bad Lines, and VC is
	// incremented without the 10 bit wrap the real counter has, so the
	// c-access runs off the end of colour RAM. That is a real bug, but
	// not this one.
	copy(Ram()[0x0800:], []byte{
		0xE6, 0x20, // INC $20
		0xA5, 0x20, // LDA $20
		0x8D, 0x21, 0xD0, // STA $D021
		0x8D, 0x22, 0xD0, // STA $D022
		0x8D, 0x23, 0xD0, // STA $D023
		0x8D, 0x24, 0xD0, // STA $D024
		0x8D, 0x16, 0xD0, // STA $D016
		0x4C, 0x00, 0x08, // JMP $0800
	})
	cpu.PC = 0x0800

	for cycle := range CyclesPerFrame {
		vic.StepCycle()

		cached, cachedForeground := vic.gdColor, vic.gdForeground
		vic.refreshGraphicsPalette()
		if vic.gdColor != cached || vic.gdForeground != cachedForeground {
			t.Fatalf("cycle %d (raster %d dot %d, mode %d multicolor=%v): palette held %v/%#02x, rebuilding from the same state gives %v/%#02x - something changed an input without rebuilding",
				cycle, vic.rasterLine, vic.dot, vic.graphicsMode, vic.multicolor,
				cached, cachedForeground, vic.gdColor, vic.gdForeground)
		}
	}
}

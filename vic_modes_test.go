package tiny64

import "testing"

func TestVICGraphicsModeColors(t *testing.T) {
	v := &VICII{
		background:         [4]uint8{1, 2, 3, 4},
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
	cia.setVICBank(0)

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
	cia.setVICBank(0)

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

// mayReload is what stepLine's run structure tells reloadDot: whether this
// bus cycle is one the graphics sequencer can take up a result in. The
// tests below park the beam by hand, so they work it out from the beam.
func mayReload(v *VICII) bool {
	slot := v.slot
	return slot >= reloadFirstSlot && slot < reloadSlotAfter
}

func TestVICReloadDotMatchesPerDotRule(t *testing.T) {
	// reloadDot is asked at the head of a bus cycle, never part-way
	// through one - stepLine and stepCycle both call it with the beam on a
	// boundary. That is what lets it take the caller's slot instead of
	// dividing the beam back down to one, so the sweep stays on
	// boundaries too.
	for start := uint16(0); start < DotsPerLine; start += DotsPerCycle {
		for control := 0; control < 256; control++ {
			v := VICII{slot: start / DotsPerCycle, control2: uint8(control)}
			// Preserve the old per-dot rule independently of graphicsReloadPhase.
			phase := uint16(control & 7)
			if control&0x10 != 0 && phase == 7 {
				phase = 0
			}
			want := uint16(0xFFFF)
			matches := 0
			// The caller decides whether the cycle can reload; the old
			// rule's slot range is that decision, so it is made here.
			got := v.reloadDot(v.Dot(), mayReload(&v))
			for offset := uint16(0); offset < DotsPerCycle; offset++ {
				// The cycle beginning at start acts on these dots.
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
			if got != noReloadDot && (got < 48 || got >= 368 || got < start || got >= start+DotsPerCycle) {
				t.Fatalf("start=%d control2=%#02x: reload=%d outside cell/cycle bounds",
					start, control, got)
			}
		}
	}
}

func TestVICXScrollDelaysGraphicsReload(t *testing.T) {
	// Dot 48 is the boundary of the first character cell of the display
	// window, and where an unscrolled sequencer takes up its g-access
	// result. reloadDot is asked at the head of the cycle that paints the
	// dot, and dot 48's cycle is the one that begins there.
	unscrolled := &VICII{}
	unscrolled.slot = 6
	if got := unscrolled.reloadDot(unscrolled.Dot(), mayReload(unscrolled)); got != 48 {
		t.Fatalf("unscrolled reload dot = %d, want the cell boundary at 48", got)
	}

	// XSCROLL=3 moves the reload three dots into the cell, to 51.
	v := &VICII{control2: 3, gdPending: 0xFF, videoBufferPending: 0x0100}
	v.slot = 6
	if got := v.reloadDot(v.Dot(), mayReload(v)); got == 48 {
		t.Fatal("sequencer reloaded on the cell boundary with XSCROLL=3")
	}
	if got := v.reloadDot(v.Dot(), mayReload(v)); got != 51 {
		t.Fatalf("reload dot = %d with XSCROLL=3, want 51", got)
	}

	// And the data is actually taken up there.
	v.slot = 51 / DotsPerCycle
	v.dotclock(51, 51, false)
	// The sequencer holds two bits per dot, so the pending $FF is 0x5555
	// once widened - see expandGraphicsData. dotclock reloads and then
	// paints the same dot, which shifts the first pixel out of it, so what
	// is left is 0x5555 shifted up two bits, which is 0x5554.
	if v.gdSequencer != 0x5554 || v.videoBuffer != 0x0100 {
		t.Fatalf("sequencer=%#04x buffer=%#04x at dot %d, want pending graphics data",
			v.gdSequencer, v.videoBuffer, v.Dot())
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
	v.slot = 6 // the cycle covering dots 48 to 55
	if got := v.reloadDot(v.Dot(), mayReload(v)); got == 55 {
		t.Fatal("sequencer reloaded at dot 55 in multicolor mode with XSCROLL=7")
	}
	v.slot = 7
	if got := v.reloadDot(v.Dot(), mayReload(v)); got != 56 {
		t.Fatalf("multicolor XSCROLL=7 reload dot = %d, want 56", got)
	}

	v.slot = 7
	v.dotclock(56, 56, false)
	// $FF widened two bits to the dot. This character is not multicolor
	// itself - bit 11 of the video buffer is clear - so it widens as a
	// standard one even though MCM is set. As above, the dot that reloads
	// is also painted, so one pixel has already shifted out: 0x5554.
	if v.gdSequencer != 0x5554 || v.videoBuffer != 0x0100 {
		t.Fatalf("sequencer=%#04x buffer=%#04x at dot %d, want pending graphics data",
			v.gdSequencer, v.videoBuffer, v.Dot())
	}

	// At standard resolution the same XSCROLL reloads at 55, one dot
	// earlier, because there is no pair to finish.
	v = &VICII{control2: 7, gdPending: 0xA5}
	v.slot = 6
	if got := v.reloadDot(v.Dot(), mayReload(v)); got != 55 {
		t.Fatalf("standard-resolution XSCROLL=7 reload dot = %d, want 55", got)
	}
	v.slot = 55 / DotsPerCycle
	v.dotclock(55, 55, false)
	// $A5 widened two bits to the dot is 0x4411, less the one pixel the
	// same dotclock call paints.
	if v.gdSequencer != 0x1044 {
		t.Fatal("standard-resolution XSCROLL=7 did not reload at dot 55")
	}
}

func TestVICXScrollWriteReloadsAtCurrentDot(t *testing.T) {
	// A register write only ever happens inside the CPU's Phi2, which
	// falls four dots into a bus cycle. So the only XSCROLL a write can
	// land on the reload dot of is the one whose phase is that midpoint -
	// writing any other moves the reload to a dot this cycle has either
	// already passed or has still to reach.
	//
	// v.Dot() holds the cycle's first dot while the CPU runs, which is what
	// the dot path leaves behind it; slot 6 is inside the reload window.
	v := &VICII{
		slot:               6,
		control2:           7,
		gdPending:          0xA5,
		videoBufferPending: 0x0D06,
	}

	v.WriteRegister(0xD016, DotsPerCycle/2)

	if v.gdSequencer != 0x4411 || v.videoBuffer != 0x0D06 { // $A5 widened
		t.Fatalf("sequencer=%#04x buffer=%#04x after same-dot XSCROLL write, want pending graphics data",
			v.gdSequencer, v.videoBuffer)
	}
}

// openDisplayVIC returns a VIC-II parked mid-screen with the display
// window open: raster 100 is below the top comparison, so the vertical
// border flip-flop is clear and rule 6 can reset the main one at the left
// comparison. Reset seeds verticalBorder set because raster 0 is in the
// upper border.
func openDisplayVIC(control2 uint8) *VICII {
	v := &VICII{}
	v.Reset()
	v.rasterLine = 100
	v.control1 = 0x1B // DEN=1, RSEL=1, YSCROLL=3
	v.control2 = control2
	v.verticalBorder = false
	v.syncLineVisibility()
	return v
}

// TestVICSideBorderOpen40To38Trick drives the side-border trick the way a
// demo does. Section 3.9's rule 1 sets the main border flip-flop when the X
// coordinate *reaches* a right comparison value, and the two values belong
// to different CSEL settings: $14F to 38 columns, $158 to 40. Hold CSEL=1
// while the beam passes $14F and drop it to 0 before it reaches $158 and
// neither comparison ever sees its own value, so rule 1 never fires and the
// border stays open for the rest of the line.
//
// Nothing here goes back over a dot that has already been drawn: the write
// simply arrives before the comparison that reads it.
func TestVICSideBorderOpen40To38Trick(t *testing.T) {
	parkMachine(t)
	v := openDisplayVIC(csel) // 40 columns

	// Past $14F with CSEL=1, so it does not match. Rule 6 cleared the main
	// border flip-flop back at the left comparison.
	for v.Dot() <= rightEdge38 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d in 40-column mode, want false", v.Dot())
	}

	// Drop to 38 columns in the window between the two comparisons.
	v.WriteRegister(0xD016, 0)

	// $158 now finds CSEL=0, so it does not match either.
	for v.Dot() <= rightEdge40 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d after the CSEL trick, want false (side border open)", v.Dot())
	}

	// Without the trick the same beam position latches the border.
	vNormal := openDisplayVIC(csel)
	for vNormal.Dot() <= rightEdge40 {
		vNormal.StepCycle()
	}
	if !vNormal.mainBorder {
		t.Fatalf("mainBorder=false at dot %d in normal 40-column mode, want true", vNormal.Dot())
	}
}

// TestVICSideBorderOpen38To40Trick is the same trick entered from the other
// CSEL setting. Only the value standing at each comparison dot matters, not
// which mode the line started in.
func TestVICSideBorderOpen38To40Trick(t *testing.T) {
	parkMachine(t)
	v := openDisplayVIC(0) // 38 columns

	// Widen to 40 columns before $14F, so its comparison finds CSEL=1.
	v.WriteRegister(0xD016, csel)
	for v.Dot() <= rightEdge38 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d with CSEL=1, want false", v.Dot())
	}

	// Narrow back to 38 before $158, so its comparison finds CSEL=0.
	v.WriteRegister(0xD016, 0)
	for v.Dot() <= rightEdge40 {
		v.StepCycle()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d with CSEL=0, want false (side border open)", v.Dot())
	}
}

// TestVICSideBorderComparisonIsNotRevisited pins the half of rule 1 that is
// easy to lose. The comparison happens once, as the beam reaches the value;
// a $D016 write that arrives afterwards cannot unmake it, however few dots
// late it is. A model that let it would be reaching back over pixels the
// VIC has already put on the screen, which is not something the hardware
// can do - and the deferred-comparison model this replaced did exactly that.
func TestVICSideBorderComparisonIsNotRevisited(t *testing.T) {
	parkMachine(t)
	v := openDisplayVIC(csel) // 40 columns

	for v.Dot() <= rightEdge40 {
		v.StepCycle()
	}
	if !v.mainBorder {
		t.Fatalf("mainBorder=false at dot %d, want true: $158 should have matched", v.Dot())
	}

	v.WriteRegister(0xD016, 0)
	if !v.mainBorder {
		t.Fatalf("a $D016 write at dot %d reopened a border already closed at dot %d",
			v.Dot(), rightEdge40)
	}
}

// TestVICSideBorderWriteReachesTheNextComparison is the phase this all
// rests on. The 6510's Phi2 falls in the middle of the VIC's eight-dot
// slot, not at its end: VINC falls at dot 428, four dots into its own
// slot rather than on the boundary. So a write made in the slot that
// ends at dot 368 lands at dot 364 and is in place when rule 1 reads CSEL
// four dots later.
//
// When the CPU ran at the end of the slot instead, that same write landed
// at 369 - one dot past $158 - and every demo that opens the side border
// closed it instead.
func TestVICSideBorderWriteReachesTheNextComparison(t *testing.T) {
	parkMachine(t)
	v := openDisplayVIC(csel)

	for v.Dot() < rightEdge40-DotsPerCycle {
		v.StepCycle()
	}
	// One slot short of $158: the write below is the last one that can
	// still be seen by it.
	if v.Dot() != rightEdge40-DotsPerCycle {
		t.Fatalf("beam at dot %d, want %d", v.Dot(), rightEdge40-DotsPerCycle)
	}
	v.WriteRegister(0xD016, 0)

	// One cycle to finish the slot the write was made in, and one to enter
	// $158's own, where rule 1 runs on the first dot.
	v.StepCycle()
	v.StepCycle()
	if v.Dot() != rightEdge40+DotsPerCycle {
		t.Fatalf("beam at dot %d after two cycles, want %d", v.Dot(), rightEdge40+DotsPerCycle)
	}
	if v.mainBorder {
		t.Fatal("$158 did not see a CSEL write made in the preceding bus cycle")
	}
}

// TestVICSideBorderWriteAtCompareDot is the same question at the left
// comparison, where rule 6 resets the main border flip-flop. A write in the
// dot before it is seen; one in the dot after is not, because the beam has
// already gone past.
func TestVICSideBorderWriteAtCompareDot(t *testing.T) {
	v := openDisplayVIC(0) // 38 columns, so leftComp40 does not match yet
	v.slot = leftComp40/DotsPerCycle - 1

	v.WriteRegister(0xD016, csel)

	// borderCompare owns the left comparison, so run just that rather than
	// a whole cycle around it.
	v.slot = leftComp40 / DotsPerCycle
	v.borderCompare(v.Dot())
	if v.mainBorder {
		t.Fatal("mainBorder=true: the left comparison missed a CSEL write made one dot earlier")
	}

	// The other side of it: past the comparison, the same write is inert.
	vLate := openDisplayVIC(0)
	vLate.slot = leftComp40 / DotsPerCycle
	vLate.WriteRegister(0xD016, csel)
	if !vLate.mainBorder {
		t.Fatalf("a $D016 write at dot %d opened a border whose comparison had already run",
			leftComp40)
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
				return v.background[0], false
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
				return v.background[0], false
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
				return v.background[0], false
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
	v.background = [4]uint8{0x01, 0x02, 0x03, 0x04} // $D021-$D024

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
	vic.slot = 6
	vic.control1 = 0x1B // DEN=1, RSEL=1, YSCROLL=3
	vic.control2 = 0x08
	vic.syncLineVisibility()
	// Reset seeds the border flip-flops set, because raster 0 is in the
	// upper border; raster 100 is inside the display window.
	vic.mainBorder, vic.verticalBorder = false, false
	vic.background[0] = 6
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
				cycle, vic.rasterLine, vic.Dot(), vic.graphicsMode, vic.multicolor,
				cached, cachedForeground, vic.gdColor, vic.gdForeground)
		}
	}
}

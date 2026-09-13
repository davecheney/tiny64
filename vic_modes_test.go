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

func TestVICXScrollDelaysGraphicsReload(t *testing.T) {
	v := &VICII{control2: 3, gdPending: 0xFF, videoBufferPending: 0x0100}

	v.dot = 48 // start of slot 6, where unscrolled data would reload
	v.advanceGraphicsData()
	if v.gdSequencer != 0 {
		t.Fatalf("sequencer reloaded at dot %d with XSCROLL=3", v.dot)
	}

	v.dot = 51
	v.advanceGraphicsData()
	if v.gdSequencer != 0xFF || v.videoBuffer != 0x0100 {
		t.Fatalf("sequencer=%#02x buffer=%#04x at dot %d, want pending graphics data",
			v.gdSequencer, v.videoBuffer, v.dot)
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

	if v.gdSequencer != 0xA5 || v.videoBuffer != 0x0D06 {
		t.Fatalf("sequencer=%#02x buffer=%#04x after same-dot XSCROLL write, want pending graphics data",
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
	for v.dot != rightComp38-1 {
		v.StepDot()
	}
	v.StepDot()
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d in 40-column mode, want false", rightComp38)
	}
	v.StepDot()

	// Switch CSEL to 0 after the 38-column comparison and before the
	// 40-column comparison.
	v.WriteRegister(0xD016, 0x00) // CSEL=0

	// Since CSEL is now 0, the 40-column comparison is bypassed.
	for v.dot != rightComp40-1 {
		v.StepDot()
	}
	v.StepDot()
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
		vNormal.StepDot()
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
		v.StepDot()
	}
	if v.mainBorder {
		t.Fatalf("mainBorder=true at dot %d with CSEL=1, want false", rightComp38)
	}
	v.StepDot()

	// Switch back to CSEL=0 before the 40-column comparison.
	v.WriteRegister(0xD016, 0x00) // CSEL=0 (38 cols)

	// Since CSEL=0, the 40-column comparison will not trigger it either.
	for v.dot != rightComp40 {
		v.StepDot()
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

	v.StepDot() // reaches the 40-column left compare with CSEL still clear
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

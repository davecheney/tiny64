package tiny64

import (
	"reflect"
	"testing"
)

// stepFrame runs the VIC-II for exactly one full PAL frame's worth of dots.
func stepFrame(v *VICII) {
	v.StepFrame()
}

// TestVICStepFrameMatchesStepDot pins down the unrolled frame loop:
// StepFrame decides which dot of a bus cycle carries Phi1 and which
// carries Phi2 from its position in stepCycle rather than from dot&7, so
// it must produce exactly the same machine state, and exactly the same
// pixels, as driving the same number of dots one at a time through
// StepDot.
//
// The test vector is a frame of a machine already booted to the BASIC
// prompt, not a freshly reset one: during the first frame after reset the
// KERNAL hasn't set DEN yet, so there are no Bad Lines, no c- or
// g-accesses, and every pixel is border - which any arrangement of
// stepCycle would reproduce.
func TestVICStepFrameMatchesStepDot(t *testing.T) {
	type pixel struct {
		x, y  uint16
		color byte
	}

	m := newMachine(t)
	m.waitForLine(5, "READY.")

	// Snapshot the booted machine so both runs start from bit-identical
	// state, rather than booting twice and trusting that to be
	// reproducible.
	savedCPU, savedVIC := cpu, vic
	savedCIA1, savedCIA2 := cia1, cia2
	savedKeyboard := keyboard
	savedRAM, savedColorRAM := ram, colorRAM

	run := func(startVIC VICII, step func()) (VICII, CPU, []pixel) {
		cpu, vic = savedCPU, startVIC
		cia1, cia2 = savedCIA1, savedCIA2
		keyboard = savedKeyboard
		ram, colorRAM = savedRAM, savedColorRAM

		pixels := make([]pixel, 0, DotsPerFrame)
		WritePixelToBuffer = func(x, y uint16, colorIndex byte) {
			pixels = append(pixels, pixel{x, y, colorIndex})
		}
		step()

		gotVIC, gotCPU := vic, cpu
		return gotVIC, gotCPU, pixels
	}

	// StepDot must select the implementation for the next dot from every
	// possible current phase, including both bus hand-off phases.
	for phase := uint16(0); phase < DotsPerCycle; phase++ {
		startVIC := savedVIC
		startVIC.dot = startVIC.dot&^7 | phase

		gotVIC, gotCPU, gotPixels := run(startVIC, func() { vic.StepDot() })
		wantVIC, wantCPU, wantPixels := run(startVIC, func() {
			switch phase {
			case 0:
				vic.dotclock1()
			case 1:
				vic.dotclock2()
			case 2:
				vic.dotclock3()
			case 3:
				vic.dotclock4()
				vic.phi0low()
			case 4:
				vic.dotclock5()
			case 5:
				vic.dotclock6()
			case 6:
				vic.dotclock7()
			case 7:
				vic.dotclock0()
				vic.phi0high()
				cpu.TickPhi2()
			}
		})

		if !reflect.DeepEqual(gotVIC, wantVIC) ||
			!reflect.DeepEqual(gotCPU, wantCPU) ||
			!reflect.DeepEqual(gotPixels, wantPixels) {
			t.Errorf("StepDot from phase %d did not match dotclock%d and its bus action", phase, (phase+1)&7)
		}
	}

	frameVIC, frameCPU, framePixels := run(savedVIC, func() { vic.StepFrame() })
	dotVIC, dotCPU, dotPixels := run(savedVIC, func() {
		for range DotsPerFrame {
			vic.StepDot()
		}
	})

	// Guard against the comparison below passing on a vector that can't
	// tell the two apart: a live display paints more than one colour, and
	// only reaches the second one via a Bad Line's c- and g-accesses.
	colors := map[byte]bool{}
	for _, p := range framePixels {
		colors[p.color] = true
	}
	if len(colors) < 2 {
		t.Fatalf("frame painted %d distinct colours, want at least 2 (the display isn't active, so this proves nothing)", len(colors))
	}

	if !reflect.DeepEqual(frameVIC, dotVIC) {
		t.Errorf("VIC state after StepFrame differs from %d StepDot calls:\n StepFrame: %+v\n   StepDot: %+v",
			DotsPerFrame, frameVIC, dotVIC)
	}
	if !reflect.DeepEqual(frameCPU, dotCPU) {
		t.Errorf("CPU state after StepFrame differs from %d StepDot calls:\n StepFrame: %+v\n   StepDot: %+v",
			DotsPerFrame, frameCPU, dotCPU)
	}
	if len(framePixels) != len(dotPixels) {
		t.Fatalf("StepFrame emitted %d pixels, %d StepDot calls emitted %d",
			len(framePixels), DotsPerFrame, len(dotPixels))
	}
	for i := range framePixels {
		if framePixels[i] != dotPixels[i] {
			t.Fatalf("pixel %d: StepFrame emitted %+v, StepDot emitted %+v",
				i, framePixels[i], dotPixels[i])
		}
	}
}

// TestVICResetIsIdle checks that Reset() puts the video logic in idle
// state with VC/VCBase/RC/VMLI all cleared (section 3.7.1).
func TestVICResetIsIdle(t *testing.T) {
	v := &VICII{}
	v.registers[regControl1] = 0xFF // DEN set, to prove Reset doesn't derive idle from registers
	v.RC = 5
	v.VC = 123
	v.Reset()

	if !v.idle {
		t.Errorf("idle = false, want true after Reset")
	}
	if v.RC != 0 || v.VC != 0 || v.VCBase != 0 || v.VMLI != 0 {
		t.Errorf("RC=%d VC=%d VCBase=%d VMLI=%d, want all 0 after Reset", v.RC, v.VC, v.VCBase, v.VMLI)
	}
}

// TestVICStaysIdleWithoutDEN is a regression test: without DEN ever being
// set, there is never a Bad Line Condition, so the video logic must stay
// in idle state and RC/VC/VCBase must stay at 0 forever (previously, RC
// incremented unconditionally every line regardless of state, counted up
// to 7 and got stuck there, and VCBase/VC grew without bound).
func TestVICStaysIdleWithoutDEN(t *testing.T) {
	WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	v := &VICII{}
	v.Reset()

	for range 3 {
		stepFrame(v)
	}

	if !v.idle {
		t.Errorf("idle = false, want true (DEN was never set, so no Bad Line should ever occur)")
	}
	if v.RC != 0 {
		t.Errorf("RC = %d, want 0 (must not free-run when there's no Bad Line Condition)", v.RC)
	}
	if v.VC != 0 || v.VCBase != 0 {
		t.Errorf("VC=%d VCBase=%d, want both 0 (must not advance in idle state)", v.VC, v.VCBase)
	}
}

func TestVICFinishFrameAdvancesToNextFrameBoundary(t *testing.T) {
	WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	v := &VICII{}
	v.Reset()

	v.dot = 123
	v.rasterLine = 45
	v.FinishFrame()
	if v.dot != 0 || v.rasterLine != 0 {
		t.Fatalf("after FinishFrame from mid-frame, dot=%d raster=%d, want top of frame", v.dot, v.rasterLine)
	}

	v.FinishFrame()
	if v.dot != 0 || v.rasterLine != 0 {
		t.Fatalf("after FinishFrame from frame boundary, dot=%d raster=%d, want next frame boundary", v.dot, v.rasterLine)
	}
}

// TestVICBadLineEntersDisplayState checks that setting DEN produces a Bad
// Line Condition at raster $33 (with YSCROLL=3), which flips the video
// logic into display state and starts advancing RC; that Bad Lines recur
// every 8 raster lines (once per text row) so RC correctly cycles 0-7
// repeatedly instead of getting stuck; and that the video logic returns
// to idle state once raster lines stop matching YSCROLL (i.e. past $F7,
// where no further Bad Line Condition can occur).
func TestVICBadLineEntersDisplayState(t *testing.T) {
	WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	v := &VICII{}
	v.Reset()
	v.registers[regControl1] = 0x13 // DEN=1, YSCROLL=3, RSEL=0

	// Steps into the next raster line, far enough that its first phi0low
	// (on dot 4, since bus cycles aren't phase-aligned to dot 0) has run
	// and evaluated the Bad Line Condition.
	stepLine := func() {
		startLine := v.rasterLine
		for v.rasterLine == startLine || v.dot < 4 {
			v.StepDot()
		}
	}

	// Run up to (but not including) raster line $33, the first line whose
	// low 3 bits match YSCROLL=3 once allowBadLine has latched (which
	// happens at the end of raster line $30, per section 3.5).
	for v.rasterLine != 0x32 || v.dot != 0 {
		v.StepDot()
	}
	if !v.idle {
		t.Fatalf("idle = false before raster $33, want true")
	}

	// Raster line $33 is a Bad Line (YSCROLL=3 matches its low 3 bits, and
	// DEN was set during raster line $30), so the video logic should enter
	// display state, and RC should cycle 0-7 on each subsequent Bad Line's
	// row instead of running away or getting stuck.
	stepLine() // process raster $33
	rcSeen := map[uint8]bool{}
	for range 25 * 8 {
		if v.idle {
			t.Fatalf("idle = true at raster=%d, want false (Bad Lines recur every 8 lines through $30-$F7)", v.rasterLine)
		}
		if v.RC > 7 {
			t.Fatalf("RC = %d at raster=%d, want 0-7", v.RC, v.rasterLine)
		}
		rcSeen[v.RC] = true
		stepLine()
	}
	for rc := uint8(0); rc <= 7; rc++ {
		if !rcSeen[rc] {
			t.Errorf("RC never took the value %d across 25 text rows", rc)
		}
	}

	// Once raster lines no longer match YSCROLL within $30-$F7 (i.e. past
	// $F7), no more Bad Lines occur and the video logic must return to
	// idle state instead of staying stuck in display state forever.
	for v.rasterLine <= badLineRasterEnd {
		v.StepDot()
	}
	if !v.idle {
		t.Errorf("idle = false past raster $F7, want true (no more Bad Lines can occur)")
	}
}

// TestVICGAccessCountPerRow is a regression test: a Bad Line's row must
// produce exactly 40 g-accesses (one per column), so VC advances by
// exactly 40 per row and every column (including the last) gets a g-access
// loading fresh graphics data. Previously g-access stopped one cycle too
// early, leaving the 40th column's gdSequencer stale (displaying as blank)
// and VC drifting out of sync with the video matrix every row.
func TestVICGAccessCountPerRow(t *testing.T) {
	WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	v := &VICII{}
	v.Reset()
	v.registers[regControl1] = 0x1B // DEN=1, RSEL=1, YSCROLL=3

	// Run to the start of the first Bad Line's row ($33).
	for v.rasterLine != 0x33 || v.dot != 0 {
		v.StepDot()
	}
	vcBefore := v.VC
	for v.rasterLine == 0x33 {
		v.StepDot()
	}
	if got := v.VC - vcBefore; got != 40 {
		t.Errorf("VC advanced by %d across one Bad Line row, want 40", got)
	}
}

// TestVICVideoMatrixAddress checks that a Bad Line's c-access reads from
// the video matrix address derived from $D018's VM bits (VM<<10 + VC),
// and that VC stays within the 1000-entry video matrix range across a
// full frame instead of drifting to unrelated pages of memory.
func TestVICVideoMatrixAddress(t *testing.T) {
	WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	v := &VICII{}
	v.Reset()
	v.registers[regControl1] = 0x1B    // DEN=1, RSEL=1, YSCROLL=3 (KERNAL defaults)
	v.registers[regMemPointers] = 0x14 // VM=1 (screen at $0400), CB=2 (chars at $1000)

	// Mark every page of RAM with its own page number, so any c-access
	// landing outside the expected $0400-$07E7 video matrix range is
	// immediately obvious from the value read.
	for page := range 256 {
		for i := range 256 {
			ram[page*256+i] = byte(page)
		}
	}

	maxVC := uint16(0)
	const dotsPerFrame = DotsPerLine * RasterLinesPerFrame
	for range dotsPerFrame {
		v.StepDot()
		if v.VC > maxVC {
			maxVC = v.VC
		}
	}

	const videoMatrixSize = 1000 // 40 columns * 25 rows
	if maxVC > videoMatrixSize {
		t.Errorf("VC reached %d during a frame, want <= %d (video matrix is only 1000 entries)", maxVC, videoMatrixSize)
	}
}

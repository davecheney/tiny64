package tiny64

import "testing"

// TestStepFrameNeedsALineBoundary pins the requirement that lets StepFrame
// walk whole lines. The mid-line case used to be handled by a second loop
// that stepped a cycle at a time; nothing but a test ever reached it, and
// carrying it meant StepFrame could not assume where a line starts.
func TestStepFrameNeedsALineBoundary(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()

	// A frame is a whole number of lines, so driving frames keeps the beam
	// where StepFrame needs it however many go by.
	for range 3 {
		v.StepFrame()
		if v.slot != 0 {
			t.Fatalf("a whole frame left the beam at dot %d, not a line boundary", v.Dot())
		}
	}

	// Part-way through a line it refuses, rather than quietly walking the
	// frame with every line cut in the wrong place.
	v.StepCycle()
	if v.slot == 0 {
		t.Fatal("one bus cycle should have moved the beam off the line boundary")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("StepFrame accepted a beam part-way through a line")
			}
		}()
		v.StepFrame()
	}()
}

// TestStepFrameKeepsItsRasterLine checks the half of the old contract that
// is kept: only the position within the line is pinned, so a frame begun on
// one raster line ends on it.
func TestStepFrameKeepsItsRasterLine(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()

	for range 45 * CyclesPerLine {
		v.StepCycle()
	}
	startLine, startBeam := v.rasterLine, v.beamLine
	if startLine == 0 {
		t.Fatal("45 lines in, the raster counter should not be back at zero")
	}

	v.StepFrame()

	if v.rasterLine != startLine || v.beamLine != startBeam || v.slot != 0 {
		t.Fatalf("a frame from line %d/beam %d ended at line %d/beam %d dot %d",
			startLine, startBeam, v.rasterLine, v.beamLine, v.Dot())
	}
}

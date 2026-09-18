package tiny64

import "testing"

// TestBeamLineAgreesWithRasterWherePainted pins the invariant that lets the
// beam and $D012 be separate counters at all. They part company on VINC and
// stay apart until the beam wraps, but that window is entirely blanked, so
// no painted dot can tell the difference. If a future change moves either
// counter, or widens the visible window past VINC, this is what notices.
func TestBeamLineAgreesWithRasterWherePainted(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()

	apart := 0
	for range CyclesPerFrame * 2 {
		// The cycle about to run paints dots v.dot through v.dot+7. When
		// every one of them reaches the screen, the two counters have to
		// agree on both sides of the cycle, since the row each painted dot
		// lands in is read from beamLine and named by $D012.
		visible := v.dot+DotsPerCycle <= VisibleDotsPerLine
		if visible && v.beamLine != v.rasterLine {
			t.Fatalf("before cycle at dot %d: beamLine=%d, $D012=%d",
				v.dot, v.beamLine, v.rasterLine)
		}

		v.StepCycle()

		if visible && v.beamLine != v.rasterLine {
			t.Fatalf("after cycle ending at dot %d: beamLine=%d, $D012=%d",
				v.dot, v.beamLine, v.rasterLine)
		}
		if v.beamLine != v.rasterLine {
			apart++
		}
	}

	// And they must actually diverge somewhere, or the test proves nothing.
	if apart == 0 {
		t.Fatal("beamLine and $D012 never differed; the invariant is vacuous")
	}
}

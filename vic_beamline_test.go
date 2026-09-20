package tiny64

import "testing"

// TestBeamLineAgreesWithRasterWherePainted pins the invariant that lets the
// beam and $D012 be separate counters at all. They part company on VINC and
// stay apart until the beam wraps, but that window is entirely blanked, so
// no painted dot can tell the difference. If a future change moves either
// counter, or widens the visible window past VINC, this is what notices.
func TestBeamLineAgreesWithRasterWherePainted(t *testing.T) {
	// StepCycle clocks the CPU, the CIAs and the bus, so stepping here
	// moves the whole machine even though the counters under test are the
	// VIC's.
	saveMachine(t)
	v := &VICII{}
	v.Reset()

	apart := 0
	for range CyclesPerFrame * 2 {
		// The cycle about to run paints dots v.Dot() through v.Dot()+7. When
		// every one of them reaches the screen, the two counters have to
		// agree on both sides of the cycle, since the row each painted dot
		// lands in is read from beamLine and named by $D012.
		visible := v.Dot()+DotsPerCycle <= VisibleDotsPerLine
		if visible && v.beamLine != v.rasterLine {
			t.Fatalf("before cycle at dot %d: beamLine=%d, $D012=%d",
				v.Dot(), v.beamLine, v.rasterLine)
		}

		v.StepCycle()

		if visible && v.beamLine != v.rasterLine {
			t.Fatalf("after cycle ending at dot %d: beamLine=%d, $D012=%d",
				v.Dot(), v.beamLine, v.rasterLine)
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

// The invariant above holds because the window where the counters differ -
// VINC to the end of the line - is blanked. That is a fact about where the
// visible window ends, not about the counters, so hold it separately.
func TestNothingPaintsAfterVINC(t *testing.T) {
	if firstDot := uint16(vincSlot) * DotsPerCycle; firstDot < VisibleDotsPerLine {
		t.Fatalf("VINC is in slot %d, whose dots start at %d, inside the visible %d",
			vincSlot, firstDot, VisibleDotsPerLine)
	}
	if renderDotAfter > VisibleDotsPerLine {
		t.Fatalf("the render window ends at dot %d, past the visible %d",
			renderDotAfter, VisibleDotsPerLine)
	}
}

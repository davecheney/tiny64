package tiny64

import "testing"

// TestLineDrawabilityIsSettledBeforeAnythingPaints pins the fact stepLine
// rests on: it reads v.lineDrawable once, at the top of the line, and every
// slot that paints trusts that one read.
//
// That is only sound while the last painting slot comes before VINC, where
// the raster counter moves and the flag is recomputed. Both are constants
// here, so this is a statement about the geometry rather than about any
// particular frame - if the render window ever grew past VINC, a line could
// change its mind about painting with painting still to do, and stepLine
// would be using a stale answer for the rest of it.
func TestLineDrawabilityIsSettledBeforeAnythingPaints(t *testing.T) {
	lastPainting := renderSlotAfter - 1
	if lastPainting >= vincSlot {
		t.Fatalf("slot %d paints but VINC is at %d: stepLine's single read of "+
			"lineDrawable would go stale mid-line", lastPainting, vincSlot)
	}

	// And the three slots the border comparator can match must be inside
	// the painted run too, or stepLine's runs would skip one of them.
	for _, slot := range []uint16{borderSlotLeft, borderSlotRight38, borderSlotRight40} {
		if slot < renderFirstSlot || slot >= renderSlotAfter {
			t.Fatalf("border comparison slot %d is outside the painted run "+
				"[%d,%d)", slot, renderFirstSlot, renderSlotAfter)
		}
	}
}

// TestStepLineMatchesStepCycle checks the two ways of walking a line agree.
// stepLine bakes the answers into runs; stepCycle asks per cycle. They must
// leave the machine in the same state, or StepFrame's two paths diverge.
func TestStepLineMatchesStepCycle(t *testing.T) {
	parkMachine(t)
	ClearFrameBuffer()

	byLine := &VICII{}
	byLine.Reset()
	byCycle := &VICII{}
	byCycle.Reset()

	for line := range RasterLinesPerFrame {
		byLine.stepLine()
		for slot := uint16(0); slot < CyclesPerLine; slot++ {
			byCycle.stepCycle(slot)
		}
		if byLine.dot != byCycle.dot || byLine.beamLine != byCycle.beamLine ||
			byLine.rasterLine != byCycle.rasterLine ||
			byLine.mainBorder != byCycle.mainBorder ||
			byLine.verticalBorder != byCycle.verticalBorder {
			t.Fatalf("line %d: stepLine left dot=%d beam=%d raster=%d "+
				"border=%v/%v, stepCycle left dot=%d beam=%d raster=%d "+
				"border=%v/%v",
				line, byLine.dot, byLine.beamLine, byLine.rasterLine,
				byLine.mainBorder, byLine.verticalBorder,
				byCycle.dot, byCycle.beamLine, byCycle.rasterLine,
				byCycle.mainBorder, byCycle.verticalBorder)
		}
	}
}

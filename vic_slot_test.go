package tiny64

import "testing"

// TestSlotTracksTheBeamAcrossAFrame pins the invariant StepFrame's loop
// rests on. It counts the slot itself rather than recovering it from the
// beam, which is only sound while the two stay in lockstep - so if anything
// in a bus cycle ever moves the beam by other than DotsPerCycle, or wraps
// it somewhere other than the end of a line, this is what notices.
func TestSlotTracksTheBeamAcrossAFrame(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()

	slot := v.dot / DotsPerCycle
	for cycle := range CyclesPerFrame {
		if got := v.dot / DotsPerCycle; got != slot {
			t.Fatalf("cycle %d: beam is in slot %d but the loop counted %d",
				cycle, got, slot)
		}
		v.StepCycle()
		slot++
		if slot >= CyclesPerLine {
			slot = 0
		}
	}

	// A frame is a whole number of lines, so the beam ends where it began.
	if v.dot/DotsPerCycle != slot {
		t.Fatalf("after a frame the beam is in slot %d, want %d",
			v.dot/DotsPerCycle, slot)
	}
}

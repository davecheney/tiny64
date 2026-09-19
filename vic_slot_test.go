package tiny64

import "testing"

// TestDotIsAlwaysABusCycleBoundary pins what makes the beam position
// derivable rather than stored. The machine counts bus cycles; a caller can
// only look between them, so every horizontal position anything outside it
// can observe is a multiple of DotsPerCycle.
//
// If that ever stops holding - if some path leaves the machine part-way
// through a cycle - then Dot is lying rather than computing, and a stored
// dot would be needed again.
func TestDotIsAlwaysABusCycleBoundary(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()

	for cycle := range CyclesPerFrame {
		if got := v.Dot(); got%DotsPerCycle != 0 {
			t.Fatalf("cycle %d: Dot reported %d, not a bus-cycle boundary", cycle, got)
		}
		if got, want := v.Dot(), v.Slot()*DotsPerCycle; got != want {
			t.Fatalf("cycle %d: Dot reported %d but slot %d means %d",
				cycle, got, v.Slot(), want)
		}
		if v.Slot() >= CyclesPerLine {
			t.Fatalf("cycle %d: slot %d is past the end of a line", cycle, v.Slot())
		}
		v.StepCycle()
	}

	// A frame is a whole number of lines, so it ends where it began.
	if v.Slot() != 0 {
		t.Fatalf("a frame ended in slot %d, not at the start of a line", v.Slot())
	}
}

// TestSlotAdvancesOncePerBusCycle is the other half: the counter moves by
// one per cycle and wraps at the end of a line, which is what lets the dot
// path add a constant offset instead of counting.
func TestSlotAdvancesOncePerBusCycle(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()

	for cycle := range 3 * CyclesPerLine {
		before := v.Slot()
		v.StepCycle()
		want := before + 1
		if want >= CyclesPerLine {
			want = 0
		}
		if v.Slot() != want {
			t.Fatalf("cycle %d: slot went %d -> %d, want %d",
				cycle, before, v.Slot(), want)
		}
	}
}

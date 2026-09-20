package desktop

import (
	"testing"
	"time"
)

// TestPacerFramePeriodIsPAL holds the constant to the machine it is
// meant to model: 19656 cycles at 985248Hz, which is 50.125 frames a
// second, not the 60 or 120 a panel would have imposed.
func TestPacerFramePeriodIsPAL(t *testing.T) {
	if got, want := palFramePeriod, 19950306*time.Nanosecond; got != want {
		t.Errorf("palFramePeriod = %v, want %v", got, want)
	}

	rate := float64(time.Second) / float64(palFramePeriod)
	if rate < 50.12 || rate > 50.13 {
		t.Errorf("frame rate = %.3fHz, want 50.125Hz", rate)
	}
}

// TestPacerSleepsOutTheRestOfTheFrame is the ordinary case: a frame whose
// work took less than its period sleeps the remainder, so what the caller
// waits is the period minus what it spent, not the period.
func TestPacerSleepsOutTheRestOfTheFrame(t *testing.T) {
	var p pacer
	start := time.Now()

	// The first frame starts the clock, so it asks for a whole period.
	if got := p.advance(start); got != palFramePeriod {
		t.Errorf("first frame: advance = %v, want %v", got, palFramePeriod)
	}

	// The second took 5ms of its own, so only the rest of it is left.
	spent := 5 * time.Millisecond
	want := palFramePeriod - spent
	if got := p.advance(start.Add(palFramePeriod + spent)); got != want {
		t.Errorf("second frame: advance = %v, want %v", got, want)
	}
}

// TestPacerDoesNotAccumulateJitter is why the deadline moves by a fixed
// period instead of being measured from now: a sleep that overshot has to
// come out of the next frame, or a run drifts by the sum of every
// overshoot in it.
func TestPacerDoesNotAccumulateJitter(t *testing.T) {
	var p pacer
	start := time.Now()
	p.advance(start)

	// The sleep ran 1ms long. The next frame is correspondingly shorter,
	// and the one after that is back to a full period.
	over := time.Millisecond
	if got, want := p.advance(start.Add(palFramePeriod+over)), palFramePeriod-over; got != want {
		t.Errorf("after overshoot: advance = %v, want %v", got, want)
	}
	if got := p.advance(start.Add(2 * palFramePeriod)); got != palFramePeriod {
		t.Errorf("recovered frame: advance = %v, want %v", got, palFramePeriod)
	}
}

// TestPacerWritesOffAnOverrun covers a frame that took longer than a
// frame: the next one does not get a negative deadline it would then
// sprint to repay, it starts fresh.
func TestPacerWritesOffAnOverrun(t *testing.T) {
	var p pacer
	start := time.Now()
	p.advance(start)

	// This frame took three periods. time.Sleep treats what comes back
	// as no wait, and the deadline resets rather than staying in debt.
	late := start.Add(4 * palFramePeriod)
	if got := p.advance(late); got > 0 {
		t.Errorf("overrun: advance = %v, want not positive", got)
	}

	// Having written the debt off, the next frame is a whole one again -
	// the machine does not run fast to catch up.
	if got := p.advance(late); got != palFramePeriod {
		t.Errorf("after write-off: advance = %v, want %v", got, palFramePeriod)
	}
}

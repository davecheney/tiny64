//go:build !vicmini

package tiny64

import "testing"

// The sprite half of two probes that are not otherwise about sprites:
// cia_clock_test.go's, which counts the bus cycles the CPU was held off
// for, and vic_bench_test.go's, which checks the benchmark programs
// actually reach the work they are named for. Both want to know that the
// sprite unit did something, and a build made with -tags vicmini has no
// sprite unit to do it, so the expectation is a pair of functions rather
// than a test inside each caller.
//
// The vicmini half asserts the opposite and is just as real: a build with
// no sprite unit that still stole a cycle for sprite DMA would be wrong.

// wantSpriteDMAHolds checks the probe saw the CPU held off the bus for
// sprite DMA.
func wantSpriteDMAHolds(t *testing.T, held int) {
	t.Helper()
	if held == 0 {
		t.Error("no CPU cycles were held for sprite DMA; the probe is not covering it")
	}
}

// wantSpriteBenchmarkCycles checks loadSpriteProgram reaches both the Bad
// Line work and the sprite work it is named for.
func wantSpriteBenchmarkCycles(t *testing.T, badLine, sprite int) {
	t.Helper()
	if badLine == 0 || sprite == 0 {
		t.Errorf("loadSpriteProgram: %d Bad Line cycles and %d sprite cycles, want both", badLine, sprite)
	}
}

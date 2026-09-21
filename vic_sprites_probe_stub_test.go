//go:build vicmini

package tiny64

import "testing"

// The vicmini half of the two probes vic_sprites_probe_test.go describes.
// There is no sprite unit in this build, so what these assert is that
// nothing behaved as though there were: no cycle stolen for a DMA that
// cannot happen, and no sprite work in the benchmark program that sets
// sprites up.
//
// That is the point of asserting rather than skipping. The registers the
// sprite program writes still exist and still accept writes, so a build
// that had quietly kept some of the unit would look identical from the
// outside until one of these caught it.

// wantSpriteDMAHolds checks nothing was held off the bus for sprite DMA.
func wantSpriteDMAHolds(t *testing.T, held int) {
	t.Helper()
	if held != 0 {
		t.Errorf("%d CPU cycles were held for sprite DMA, but this build has no sprite unit to request it", held)
	}
}

// wantSpriteBenchmarkCycles checks loadSpriteProgram still reaches its Bad
// Line work, and that its sprites cost nothing.
func wantSpriteBenchmarkCycles(t *testing.T, badLine, sprite int) {
	t.Helper()
	if badLine == 0 {
		t.Errorf("loadSpriteProgram: %d Bad Line cycles, want some even without sprites", badLine)
	}
	if sprite != 0 {
		t.Errorf("loadSpriteProgram: %d sprite cycles, but this build has no sprite unit", sprite)
	}
}

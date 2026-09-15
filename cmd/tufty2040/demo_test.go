//go:build tinygo

package main

import (
	"testing"

	"github.com/davecheney/tiny64"
)

// Run without the hardware frontend using:
// go test cmd/tufty2040/demo.go cmd/tufty2040/demo_test.go
func TestDemoKeyPhasesMatchCycleDeadlines(t *testing.T) {
	t.Cleanup(func() { tiny64.Keys().ReleaseAll() })
	var d demoLoader
	d.start([]keyStroke{{key: tiny64.KeyA, shift: true}, {key: tiny64.KeyB}})
	var clock, deadline uint64
	keyIndex, pressed := 0, false
	for frame := 0; frame < 20; frame++ {
		clock += tiny64.CyclesPerFrame
		wantDone := false
		if clock >= deadline {
			if pressed {
				pressed = false
				keyIndex++
				wantDone = keyIndex == 2
			} else {
				pressed = true
			}
			deadline = clock + 40_000
		}
		if got := d.typeKeys(); got != wantDone {
			t.Fatalf("frame %d: done=%v want=%v", frame, got, wantDone)
		}
		if d.pressed != pressed || d.keyIndex != keyIndex {
			t.Fatalf("frame %d: pressed/index=%v/%d want=%v/%d", frame, d.pressed, d.keyIndex, pressed, keyIndex)
		}
		if tiny64.Keys().IsPressed(tiny64.KeyA) != (pressed && keyIndex == 0) ||
			tiny64.Keys().IsPressed(tiny64.KeyLShift) != (pressed && keyIndex == 0) ||
			tiny64.Keys().IsPressed(tiny64.KeyB) != (pressed && keyIndex == 1) {
			t.Fatalf("frame %d: keyboard matrix did not match the phase", frame)
		}
		if wantDone {
			d.reset()
			if d.delay != 0 || d.keys != nil || d.pressed || d.stage != demoWaitForPrompt {
				t.Fatal("reset retained demo state")
			}
			return
		}
	}
	t.Fatal("demo did not finish typing")
}

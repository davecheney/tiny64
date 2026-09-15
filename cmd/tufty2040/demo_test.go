//go:build tinygo

package main

import (
	"testing"

	"github.com/davecheney/tiny64"
)

// Run without the hardware frontend using:
// go test cmd/tufty2040/demo.go cmd/tufty2040/demo_test.go
func TestDemoKeyPhaseSurvivesClockWrap(t *testing.T) {
	cpu := tiny64.GetCPU()
	saved := *cpu
	t.Cleanup(func() {
		*cpu = saved
		tiny64.Keys().ReleaseAll()
	})
	for _, start := range []uint{100, ^uint(0) - 1, ^uint(0)} {
		cpu.Clock = start
		var d demoLoader
		d.start([]keyStroke{{key: tiny64.KeyA}})
		if d.typeKeys() || !tiny64.Keys().IsPressed(tiny64.KeyA) {
			t.Fatal("first call did not press the key")
		}
		cpu.Clock = start + keyPhaseCycles - 1
		if d.typeKeys() || !tiny64.Keys().IsPressed(tiny64.KeyA) {
			t.Fatal("key released before phase duration")
		}
		cpu.Clock++
		if !d.typeKeys() || tiny64.Keys().IsPressed(tiny64.KeyA) {
			t.Fatal("key did not release at phase duration")
		}
		d.reset()
		if d.phaseStart != 0 || d.phaseCycles != 0 {
			t.Fatal("reset retained a phase")
		}
	}
}

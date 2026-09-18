package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/prg"
)

// BenchmarkDemoFrames measures a frame of each snapshot fixture, running
// the same demo the fixture's PNG was captured from.
//
// The synthetic frame benchmarks in the root package drive the VIC-II hard
// but leave the rest of the machine idle - in particular loadTestProgram
// zeroes both CIAs, so their timers never run, which no C64 does once the
// KERNAL has started the jiffy clock. These boot to READY first, so every
// measured frame carries whatever the real program asks for: live CIA
// timers, raster and timer IRQs, sprite DMA, and a CPU executing demo code
// rather than a three instruction loop.
//
// Each sub-benchmark warms up to its fixture's last checkpoint frame, so
// what is measured is the demo in the state the snapshot test asserts on,
// not its loading screen.
func BenchmarkDemoFrames(b *testing.B) {
	root := *snapshotFixtureRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		b.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(root, entry.Name())
		manifest, err := loadManifest(dir)
		if err != nil {
			b.Fatalf("%s: %v", entry.Name(), err)
		}
		program, err := os.ReadFile(filepath.Join(dir, manifest.Program.File))
		if err != nil {
			b.Fatalf("%s: %v", entry.Name(), err)
		}
		warmup := 0
		for _, checkpoint := range manifest.Checkpoints {
			warmup = max(warmup, checkpoint.Frame)
		}
		b.Run(entry.Name(), func(b *testing.B) {
			// Unlike TestSnapshotFixtures this runs in-process rather than
			// forking per checkpoint, so the machine carries whatever the
			// previous sub-benchmark left behind. CPU.Reset restarts the
			// instruction sequencer but leaves the 6510's port alone, so a
			// demo that banked the KERNAL out stays banked out and the
			// next reset fetches its vector from RAM. Put the port back to
			// its power-on state (all pins inputs, floating high) first.
			c := tiny64.GetCPU()
			c.Port, c.PortDDR = 0, 0
			resetMachine(nil)
			if err := waitForReady(bootFrameLimit); err != nil {
				b.Fatal(err)
			}
			file, err := prg.ParseBytes(program)
			if err != nil {
				b.Fatal(err)
			}
			copy(tiny64.Ram()[file.LoadAddr:], file.Data)
			if _, isBASIC := prg.DecodeBASIC(file.LoadAddr, file.Data); isBASIC {
				setBASICProgram(file.LoadAddr, uint16(int(file.LoadAddr)+len(file.Data)))
				typeRUN()
			} else {
				startAt(file.LoadAddr)
			}
			for range warmup {
				tiny64.StepFrame()
			}

			b.ResetTimer()
			for range b.N {
				tiny64.StepFrame()
			}
		})
	}
}

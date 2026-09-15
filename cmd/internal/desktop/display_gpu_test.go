//go:build gpu

package desktop

import (
	"fmt"
	"os"
	"testing"

	"github.com/davecheney/tiny64"
	"github.com/hajimehoshi/ebiten/v2"
)

// Ebitengine needs its main loop on the main thread; start tests only once
// Update runs, so ReadPixels has a graphics context.
func TestMain(m *testing.M) {
	g := &shaderTestGame{run: m.Run, result: make(chan int, 1), exitCode: 1}
	ebiten.SetWindowSize(ScreenWidth, ScreenHeight)
	ebiten.SetWindowTitle("tiny64 shader tests")
	ebiten.SetRunnableOnUnfocused(true)
	if err := ebiten.RunGameWithOptions(g, &ebiten.RunGameOptions{InitUnfocused: true}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(g.exitCode)
}

type shaderTestGame struct {
	run      func() int
	result   chan int
	exitCode int
}

func (g *shaderTestGame) Update() error {
	if g.run != nil {
		run := g.run
		g.run = nil
		go func() { g.result <- run() }()
	}
	select {
	case g.exitCode = <-g.result:
		return ebiten.Termination
	default:
		return nil
	}
}

func (*shaderTestGame) Draw(*ebiten.Image) {}

func (*shaderTestGame) Layout(int, int) (int, int) {
	return ScreenWidth, ScreenHeight
}

func TestPaletteShaderPixels(t *testing.T) {
	var d display
	if err := d.init(); err != nil {
		t.Fatal(err)
	}
	defer d.shader.Deallocate()
	defer d.frame.Deallocate()
	screen := ebiten.NewImage(ScreenWidth, ScreenHeight)
	defer screen.Deallocate()
	fb := tiny64.FrameBufferIndexed()
	saved := append([]byte(nil), fb...)
	t.Cleanup(func() { copy(fb, saved) })

	compare := func(t *testing.T) {
		t.Helper()
		d.blit(screen)
		got := make([]byte, ScreenWidth*ScreenHeight*4)
		screen.ReadPixels(got)
		want := tiny64.FrameBufferRGBA()
		for i := 0; i < len(got); i += 4 {
			if actual, expected := [4]byte(got[i:i+4]), [4]byte(want[i:i+4]); actual != expected {
				t.Fatalf("pixel (%d,%d) = %v, want %v", i/4%ScreenWidth, i/4/ScreenWidth, actual, expected)
			}
		}
	}
	t.Run("lanes-and-padding", func(t *testing.T) {
		for y := range ScreenHeight {
			for x := range tiny64.FrameBufferStride {
				index := byte(0xff)
				if x < ScreenWidth {
					index = byte((x/4 + y + (x%4)*5) % 16)
				}
				fb[y*tiny64.FrameBufferStride+x] = index
			}
		}
		compare(t)
	})
	t.Run("emulated-frame", func(t *testing.T) {
		clear(tiny64.Ram())
		clear(tiny64.ColorRam())
		tiny64.ClearFrameBuffer()
		tiny64.Reset()
		for range 250 {
			tiny64.StepFrame()
		}
		compare(t)
	})
}

//go:build !sdl

package desktop

import (
	"testing"

	"github.com/davecheney/tiny64"
	"github.com/hajimehoshi/ebiten/v2"
)

// TestPaletteShaderCompiles checks the Kage source the display depends on
// is accepted by the shader compiler. Ebitengine compiles Kage to its
// intermediate representation up front, without a graphics context, so
// this catches everything but a driver rejecting the generated GLSL.
func TestPaletteShaderCompiles(t *testing.T) {
	shader, err := ebiten.NewShader(paletteShaderSrc)
	if err != nil {
		t.Fatalf("compiling palette.kage: %v", err)
	}
	shader.Deallocate()
}

func TestDisplayDimensions(t *testing.T) {
	var d display
	if err := d.init(); err != nil {
		t.Fatal(err)
	}
	defer d.shader.Deallocate()
	defer d.frame.Deallocate()
	wantX, wantY := tiny64.FrameBufferStride/4, tiny64.VisibleLines
	if got := d.frame.Bounds().Size(); got.X != wantX || got.Y != wantY {
		t.Fatalf("texture size = %v, want %dx%d", got, wantX, wantY)
	}
	if got, want := len(tiny64.FrameBufferIndexed()), 4*d.frame.Bounds().Dx()*d.frame.Bounds().Dy(); got != want {
		t.Fatalf("indexed length = %d, want %d upload bytes", got, want)
	}
	var e emulator
	if w, h := e.Layout(810, 568); w != tiny64.VisibleDotsPerLine || h != tiny64.VisibleLines {
		t.Fatalf("visible size = %dx%d, want %dx%d", w, h, tiny64.VisibleDotsPerLine, tiny64.VisibleLines)
	}
}

// TestPaletteUniform checks the palette handed to the shader is the same
// one the CPU side of the emulator draws with, in the 0..1 range shaders
// work in.
func TestPaletteUniform(t *testing.T) {
	u := paletteUniform()
	if len(u) != 16*4 {
		t.Fatalf("palette uniform has %d components, want %d", len(u), 16*4)
	}
	for i, c := range tiny64.C64Palette {
		for j, component := range c {
			if got, want := u[i*4+j], float32(component)/255; got != want {
				t.Errorf("colour %d component %d = %v, want %v", i, j, got, want)
			}
		}
	}
}

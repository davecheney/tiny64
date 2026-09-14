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

// TestPack checks each palette index arrives in the channel of the texel
// that the shader will go looking for it in. The two halves of that deal
// are written in different languages, so this is where they are held
// against each other: the arithmetic below is the shader's, in Go.
func TestPack(t *testing.T) {
	fb := make([]byte, ScreenWidth*ScreenHeight)
	for i := range fb {
		fb[i] = byte(i % 16)
	}

	d := display{packed: make([]byte, packedStride*ScreenHeight)}
	d.pack(fb)

	for y := range ScreenHeight {
		for x := range ScreenWidth {
			texel, lane := x/4, x%4
			got := d.packed[y*packedStride+texel*4+lane]
			if want := fb[y*ScreenWidth+x]; got != want {
				t.Fatalf("pixel (%d,%d) packed as %d, want %d", x, y, got, want)
			}
		}
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

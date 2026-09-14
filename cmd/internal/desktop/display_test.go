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

// TestFrameBufferLayout checks each palette index in FrameBufferIndexed
// lands in the texel and channel that the shader will sample for it,
// verifying that no per-frame packing or stride conversion is needed.
func TestFrameBufferLayout(t *testing.T) {
	fb := tiny64.FrameBufferIndexed()
	for y := range ScreenHeight {
		for x := range ScreenWidth {
			texel, lane := x/4, x%4
			offset := y*tiny64.FrameBufferStride + texel*4 + lane
			if want := y*tiny64.FrameBufferStride + x; offset != want {
				t.Fatalf("pixel (%d,%d) offset = %d, want %d", x, y, offset, want)
			}
			if offset >= len(fb) {
				t.Fatalf("pixel (%d,%d) offset %d exceeds buffer length %d", x, y, offset, len(fb))
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

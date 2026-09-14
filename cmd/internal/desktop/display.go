package desktop

import (
	_ "embed"
	"fmt"

	"github.com/davecheney/tiny64"
	"github.com/hajimehoshi/ebiten/v2"
)

//go:embed palette.kage
var paletteShaderSrc []byte

// Four palette indices ride in the four channels of one RGBA texel, so the
// texture carrying a frame is a quarter of the padded frame buffer's width.
const (
	packedWidth = tiny64.FrameBufferStride / 4
)

// display hands the GPU one byte per pixel and lets the shader turn those
// palette indices into colours, which keeps the frame buffer, the upload
// and the texture to a quarter of the size of their RGBA equivalents.
type display struct {
	shader   *ebiten.Shader
	frame    *ebiten.Image
	uniforms map[string]any
	vertices [4]ebiten.Vertex
}

func (d *display) init() error {
	shader, err := ebiten.NewShader(paletteShaderSrc)
	if err != nil {
		return fmt.Errorf("compiling palette shader: %w", err)
	}
	d.shader = shader
	d.frame = ebiten.NewImage(packedWidth, ScreenHeight)
	d.uniforms = map[string]any{"Palette": paletteUniform()}

	// One screen-filling quad. The shader works from the destination
	// position, but the source coordinates still decide which part of the
	// texture the draw call reads, so they span the whole of it.
	for i, v := range [4][4]float32{
		{0, 0, 0, 0},
		{ScreenWidth, 0, packedWidth, 0},
		{0, ScreenHeight, 0, ScreenHeight},
		{ScreenWidth, ScreenHeight, packedWidth, ScreenHeight},
	} {
		d.vertices[i] = ebiten.Vertex{
			DstX: v[0], DstY: v[1],
			SrcX: v[2], SrcY: v[3],
			ColorR: 1, ColorG: 1, ColorB: 1, ColorA: 1,
		}
	}
	return nil
}

// paletteUniform flattens C64Palette into the vec4 array the shader
// indexes, scaled into the 0..1 range shaders work in.
func paletteUniform() []float32 {
	u := make([]float32, 0, len(tiny64.C64Palette)*4)
	for _, c := range tiny64.C64Palette {
		for _, component := range c {
			u = append(u, float32(component)/255)
		}
	}
	return u
}

// blit uploads the frame as palette indices and draws it through the
// shader, which is the only place a whole pixel is ever assembled.
func (d *display) blit(screen *ebiten.Image) {
	d.frame.WritePixels(tiny64.FrameBufferIndexed())

	op := &ebiten.DrawTrianglesShaderOptions{
		Blend:    ebiten.BlendCopy,
		Uniforms: d.uniforms,
	}
	op.Images[0] = d.frame
	screen.DrawTrianglesShader(d.vertices[:], []uint16{0, 1, 2, 1, 3, 2}, d.shader, op)
}

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
// texture carrying a frame is a quarter of the picture's width. 405 isn't a
// multiple of four, so the last texel of a line is part padding.
const (
	packedWidth  = (ScreenWidth + 3) / 4
	packedStride = packedWidth * 4
)

// display hands the GPU one byte per pixel and lets the shader turn those
// palette indices into colours, which keeps the frame buffer, the upload
// and the texture to a quarter of the size of their RGBA equivalents.
type display struct {
	shader   *ebiten.Shader
	frame    *ebiten.Image
	packed   []byte
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
	d.packed = make([]byte, packedStride*ScreenHeight)
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
	d.pack(tiny64.FrameBufferIndexed())
	d.frame.WritePixels(d.packed)

	op := &ebiten.DrawTrianglesShaderOptions{
		Blend:    ebiten.BlendCopy,
		Uniforms: d.uniforms,
	}
	op.Images[0] = d.frame
	screen.DrawTrianglesShader(d.vertices[:], []uint16{0, 1, 2, 1, 3, 2}, d.shader, op)
}

// pack lays a frame of palette indices out as texels. Indices 4n..4n+3 of a
// line become the R, G, B and A channels of its nth texel, which is nothing
// more than a copy: the picture's lines run end to end, while the texture's
// are padded out to a whole number of texels.
func (d *display) pack(fb []byte) {
	for y := range ScreenHeight {
		copy(d.packed[y*packedStride:], fb[y*ScreenWidth:(y+1)*ScreenWidth])
	}
}

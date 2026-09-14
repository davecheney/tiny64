//go:build !paletted

package desktop

import (
	"github.com/davecheney/tiny64"
	"github.com/hajimehoshi/ebiten/v2"
)

// display presents frames the VIC-II has already expanded into RGBA.
type display struct{}

func (d *display) init() error { return nil }

// blit copies the raw CPU bytes straight onto the Ebitengine screen
// texture. This uses highly optimized native OS calls under the hood
// (Metal on macOS).
func (d *display) blit(screen *ebiten.Image) {
	screen.WritePixels(tiny64.FrameBufferRGBA())
}

package tiny64

import (
	"testing"

	"tinygo.org/x/drivers/pixel"
)

func TestC64PaletteRGB565BE(t *testing.T) {
	for i, rgba := range C64Palette {
		converted := uint16(pixel.NewRGB565BE(rgba[0], rgba[1], rgba[2]))
		want := [2]byte{byte(converted), byte(converted >> 8)}
		if got := c64PaletteRGB565BE[i]; got != want {
			t.Errorf("palette index %d RGBA %#v: RGB565BE bytes = %#v, want %#v from pixel.NewRGB565BE",
				i, rgba, got, want)
		}
	}
}

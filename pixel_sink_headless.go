//go:build !tinygo && headless

package tiny64

func writePixelToBuffer(x, y uint16, colorIndex byte) {}

func writePixels4ToBuffer(x, y uint16, c0, c1, c2, c3 byte) {}

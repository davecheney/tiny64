//go:build !baremetal && !headless && !pixelsink_func

package tiny64

func writePixelToBuffer(x, y uint16, colorIndex byte) {
	writePixelToIndexed(x, y, colorIndex)
}

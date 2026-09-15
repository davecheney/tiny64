//go:build !tinygo && !headless && !pixelsink_func

package tiny64

func writePixelToRow(x uint16, colorIndex byte) {
	writePixelToIndexedRow(x, colorIndex)
}

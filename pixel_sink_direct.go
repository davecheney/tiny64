//go:build !baremetal && !headless && !pixelsink_func

package tiny64

func writePixels4ToBuffer(x, y uint16, c0, c1, c2, c3 byte) {
	writePixels4ToIndexed(x, y, c0, c1, c2, c3)
}

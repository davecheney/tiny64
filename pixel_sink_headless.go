//go:build !baremetal && headless

package tiny64

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {}

func writePixels4ToBuffer(x, y uint16, c0, c1, c2, c3 byte) {}

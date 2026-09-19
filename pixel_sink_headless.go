//go:build !baremetal && headless

package tiny64

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {}

func writePixelToBuffer(x, y uint16, colorIndex byte) {}

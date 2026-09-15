//go:build !tinygo && headless

package tiny64

// ClearFrameBuffer blanks the whole frame, including the parts of it
// outside the visible picture.
func ClearFrameBuffer() {}

func selectPixelRow(y uint16) {}

func writePixelToRow(x uint16, colorIndex byte) {}

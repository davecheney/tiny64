//go:build baremetal

package tiny64

const (
	renderFirstLine = rgb565CropY
	renderLineAfter = rgb565CropY + rgb565Height
	renderFirstDot  = rgb565CropX
	renderDotAfter  = rgb565CropX + rgb565Width
)

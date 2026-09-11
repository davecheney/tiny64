//go:build !tinygo

package tiny64

const (
	renderFirstLine = FirstVisibleLine
	renderLineAfter = FirstVisibleLine + VisibleLines
	renderFirstDot  = 0
	renderDotAfter  = VisibleDotsPerLine
)

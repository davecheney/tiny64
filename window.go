//go:build !tinygo

package tiny64

// The default rendering window: everything the beam paints, so a frontend
// sees the whole picture including the full border.
//
// dot 0 is the leftmost position of a raster line (inside the left
// overscan, so not necessarily visible on a given TV); the beam moves one
// dot right per dot clock until DotsPerLine, the remainder of the line
// being the horizontal blanking interval.
//
// LastVisibleDot is 400 rather than a figure taken straight from the VIC
// Article, because the window's edges must be cycle-aligned (see the
// assertions in 6569.go). The article does not settle on one figure
// anyway: rebasing its section 3.9 table, whose X=480 is our dot 0 and
// whose last visible X=380 is our dot 404, suggests 404, while its own
// summary column says 402. 400 sits inside that range, and the dots it
// gives up are right border - the 40-column window ends at rightComp40
// (368) and sprites are unimplemented, so only flat borderColor can ever
// appear beyond it.
const (
	FirstVisibleDot = 0
	LastVisibleDot  = 400

	FirstVisibleLine = lastVBlankLine + 1
	LastVisibleLine  = firstVBlankLine - 1
)

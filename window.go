//go:build !tinygo

package tiny64

// The default rendering window: the whole picture, including enough
// border that a frontend can show the display window comfortably framed.
//
// dot 0 is the leftmost position of a raster line (inside the left
// overscan, so not necessarily visible on a given TV); the beam moves one
// dot right per dot clock until DotsPerLine, the remainder of the line
// being the horizontal blanking interval.
//
// LastVisibleDot is a policy choice, not a hardware constant. There is no
// dot at which the chip stops producing colour and no register that marks
// one: it is simply where we decide to stop painting and let stepCycle
// short-circuit the dotclocks. Only two things constrain it.
//
// It must be at least rightComp40-1 (367), the right edge of the
// 40-column display window, or we would clip actual display content. And
// it must be cycle-aligned - congruent to 0 or DotsPerCycle-1 modulo
// DotsPerCycle - so that no group of seven dots straddles the edge (see
// the assertions in 6569.go).
//
// Every dot beyond 367 is flat borderColor, since sprites are
// unimplemented and nothing else can paint there, so all legal choices
// show an identical picture differing only in how much border surrounds
// it. 400 keeps 33 dots of right border, against the 48 dots of left
// border that FirstVisibleDot = 0 implies. The picture is therefore not
// horizontally centred in the window, which is a frontend's problem to
// solve if it cares.
//
// The VIC Article is not a useful authority here. Rebasing its section
// 3.9 table gives 404 while its own summary column says 402, which is
// what one expects of a figure that was never a boundary in the first
// place.
const (
	FirstVisibleDot = 0
	LastVisibleDot  = 400

	FirstVisibleLine = lastVBlankLine + 1
	LastVisibleLine  = firstVBlankLine - 1
)

//go:build tinygo

package tiny64

// The rendering window for the Gopher Badge, whose ST7789 panel is 320x240
// and cannot show the whole picture anyway. cmd/tiny64 used to take the
// full window and throw away everything outside this rectangle, which cost
// a WritePixelToBuffer call and a dotclock body for each of the 37,084
// pixels per frame - about a third of them - that were computed and then
// discarded. Declaring the crop here instead lets stepCycle skip those
// dots outright.
//
// The horizontal edges are not chosen freely: they are exactly the
// 40-column display window, whose boundaries are the border flip-flop's
// own comparators, leftComp40 (48) and rightComp40 (368). That is what
// makes the crop safe, because narrowing the window also suppresses the
// chip state that dotclock0 and dotclock7 update behind their visibility
// tests, and every such update either falls inside this window or cannot
// be observed from within it:
//
//   - the g-access commit runs on slots 6-45, i.e. dots 48-360, all
//     inside;
//   - leftComp40 (48), leftComp38 (55) and rightComp38 (359) are all
//     inside;
//   - rightComp40 (368) is not, so mainBorder is no longer set there.
//     Nothing inside the window reads it before dot 48 of the next line
//     re-establishes it, so within the crop it is still false exactly
//     across a display line and true exactly across a 38-column border;
//   - vertically, topComp (51/55) and bottomComp (247/251) are inside, so
//     the vertical border flip-flop still flips on the right lines even
//     though lines 16-30 and 271-299 are skipped entirely. Those lines
//     are always vertical border regardless of RSEL, so the dots they
//     would have painted are flat borderColor.
//
// The cost is that a program driving open-border or mid-line CSEL effects
// would leave mainBorder in a different state than on hardware. Such
// effects are only visible in the border, which this build does not
// display, and sprites - the usual reason to open it - are unimplemented.
//
// Both edges are cycle-aligned as the assertions in 6569.go require:
// 48 is a multiple of DotsPerCycle and 367 is DotsPerCycle-1 modulo it.
const (
	FirstVisibleDot = leftComp40      // 48
	LastVisibleDot  = rightComp40 - 1 // 367, so the window is 320 dots

	// Centre the 25-row display window (raster 51-250) in 240 lines,
	// leaving 20 lines of border above and below.
	FirstVisibleLine = 31
	LastVisibleLine  = FirstVisibleLine + 240 - 1
)

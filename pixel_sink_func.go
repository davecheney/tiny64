//go:build !tinygo && !headless && pixelsink_func

package tiny64

// This sink is the indexed one with the pixel write reached through a
// function value rather than called directly, so the cost of that
// indirection can be measured against it.

var writePixelToBuffer = writePixelToIndexed

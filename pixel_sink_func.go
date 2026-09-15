//go:build !tinygo && !headless && pixelsink_func

package tiny64

// This sink is the indexed one with the pixel write reached through a
// function value rather than called directly, so the cost of that
// indirection can be measured against it. Only the per-pixel write is
// indirected: selectPixelRow runs once a raster line, where the cost of a
// call is not what is being measured.

var writePixelToRow = writePixelToIndexedRow

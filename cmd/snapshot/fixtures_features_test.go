//go:build !vicmini

package main

// vicFeatures is what this build's VIC-II can draw, against the names a
// fixture manifest uses in its "uses" list. A full build has all of it.
//
// It pairs with fixtures_features_char_test.go. The point of the pair is
// that the fixture corpus is shared: the same manifests are read by both
// configurations, and each skips what it cannot render rather than
// carrying its own list of files.
var vicFeatures = map[string]bool{
	"sprites":        true,
	"graphics-modes": true,
}

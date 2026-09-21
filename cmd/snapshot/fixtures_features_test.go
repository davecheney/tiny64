//go:build !vicmini

package main

// vicDrawsEverything reports whether this build's VIC-II can draw every
// fixture in the corpus. A full one can.
//
// It pairs with fixtures_features_char_test.go. The point of the pair is
// that the corpus is shared: the same manifests are read by both
// configurations, and each skips what it cannot render rather than
// carrying its own list of files.
const vicDrawsEverything = true

//go:build vicmini

package main

// vicDrawsEverything reports whether this build's VIC-II can draw every
// fixture in the corpus.
//
// It cannot. -tags vicmini has no sprite unit and knows standard
// character mode alone, so the only fixtures it can be held to are the
// ones whose manifests ask for nothing - which today is maze, and which
// is the reason that fixture exists.
const vicDrawsEverything = false

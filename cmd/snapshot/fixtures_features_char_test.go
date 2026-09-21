//go:build vicmini

package main

// vicFeatures is what this build's VIC-II can draw, against the names a
// fixture manifest uses in its "uses" list.
//
// It is empty. -tags vicmini has no sprite unit and knows standard
// character mode alone, so the only fixtures it can be held to are the
// ones that name nothing - which today is maze, and which is the reason
// that fixture exists.
var vicFeatures = map[string]bool{}

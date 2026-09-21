//go:build tufty2040 || gopher_badge

package main

import (
	"bytes"
	"embed"

	"github.com/davecheney/tiny64"
)

// assets holds the demo's disk/PRG images. mazePRG is the classic one-line
// Commodore 64 maze:
//
//	10 PRINT CHR$(205.5+RND(1));:GOTO 10
//
// It prints alternating diagonal glyphs indefinitely. It is kept as a raw
// PRG file (tokenized BASIC, starting with its two-byte load address) so it
// is loaded through the KERNAL rather than injected directly into the C64's
// memory.
//
//go:embed assets/*.prg
var assets embed.FS

func mustReadAsset(name string) []byte {
	data, err := assets.ReadFile(name)
	if err != nil {
		panic(err.Error())
	}
	return data
}

var mazePRG = mustReadAsset("assets/maze.prg")

// mazeRunning reports whether the autostarted load actually put the program
// in memory. Without it a failed autostart would leave the machine sitting
// at READY., which still runs frames and so still looks like it works.
func mazeRunning() bool {
	program := mazePRG[2:]
	return bytes.Equal(tiny64.Ram()[0x0801:0x0801+len(program)], program)
}

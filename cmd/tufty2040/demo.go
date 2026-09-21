//go:build tufty2040

package main

import (
	"bytes"

	"github.com/davecheney/tiny64"
)

// mazePRG is the classic one-line Commodore 64 maze:
//
//	10 PRINT CHR$(205.5+RND(1));:GOTO 10
//
// It prints alternating diagonal glyphs indefinitely. The program is kept as
// tokenized BASIC so it is loaded through the KERNAL rather than injected
// directly into the C64's memory.
var mazePRG = []byte{
	0x01, 0x08, // load address $0801
	0x1B, 0x08, // address of the end-of-program marker
	0x0A, 0x00, // line 10
	0x99, ' ', 0xC7, '(', '2', '0', '5', '.', '5', 0xAA, 0xBB, '(', '1', ')', ')', ';', ':', 0x89, ' ', '1', '0',
	0x00,       // end of line 10
	0x00, 0x00, // end of program
}

// mazeRunning reports whether the autostarted load actually put the program
// in memory. Without it a failed autostart leaves the machine sitting at
// READY., which still steps frames and so still reports a frame time: the
// board looks like it is working and the number means something else
// entirely. Every measurement taken off this build should be read next to
// it.
func mazeRunning() bool {
	program := mazePRG[2:]
	return bytes.Equal(tiny64.Ram()[0x0801:0x0801+len(program)], program)
}

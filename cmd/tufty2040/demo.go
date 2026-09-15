//go:build tinygo

package main

import (
	"bytes"
	"embed"

	"github.com/davecheney/tiny64"
)

// tick runs once per PAL frame. A 40,000-cycle phase previously became
// eligible on the third frame (3 * 19,656 cycles), so skip two ticks.
const keyPhaseFrames = 2

type keyStroke struct {
	key   tiny64.Key
	shift bool
}

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

// loadMaze types the DOS wedge shortcut up-arrow + * + RETURN. `*` is the
// CBM DOS wildcard for "the first/only program on this disk", so this loads
// whatever is attached to the device rather than a hardcoded name. The
// wedge's up-arrow path auto-queues RUN into the KERNAL keyboard buffer once
// the load finishes, so no explicit RUN (or LIST) keystrokes are needed.
var loadMaze = []keyStroke{
	{key: tiny64.KeyUpArrow}, {key: tiny64.KeyAsterisk}, {key: tiny64.KeyReturn},
}

type demoStage uint8

const (
	demoWaitForPrompt demoStage = iota
	demoLoading
	demoWaitForProgram
	demoDone
)

// demoLoader types LOAD and RUN with human-length keyboard transitions. It
// gates RUN on the actual tokenized program in RAM instead of a wall-clock
// delay, so the sequence remains correct if IEC transfer timing changes.
type demoLoader struct {
	stage    demoStage
	keys     []keyStroke
	keyIndex int
	pressed  bool
	delay    uint8
}

func (d *demoLoader) start(keys []keyStroke) {
	d.keys = keys
	d.keyIndex = 0
	d.pressed = false
	d.delay = 0
}

func (d *demoLoader) typeKeys() bool {
	if d.delay != 0 {
		d.delay--
		return false
	}
	if d.pressed {
		tiny64.Keys().ReleaseAll()
		d.pressed = false
		d.keyIndex++
		d.delay = keyPhaseFrames
		return d.keyIndex == len(d.keys)
	}

	key := d.keys[d.keyIndex]
	if key.shift {
		tiny64.Keys().Press(tiny64.KeyLShift)
	}
	tiny64.Keys().Press(key.key)
	d.pressed = true
	d.delay = keyPhaseFrames
	return false
}

func basicPrompt() bool {
	const (
		screen = 0x0400
		cols   = 40
		rows   = 25
	)
	prompt := []byte{0x12, 0x05, 0x01, 0x04, 0x19, '.'} // READY.
	ram := tiny64.Ram()
	for row := range rows {
		start := screen + row*cols
		if bytes.Equal(ram[start:start+len(prompt)], prompt) {
			return true
		}
	}
	return false
}

func mazeLoaded() bool {
	program := mazePRG[2:]
	return bytes.Equal(tiny64.Ram()[0x0801:0x0801+len(program)], program)
}

func (d *demoLoader) reset() {
	d.stage = demoWaitForPrompt
	d.keys = nil
	d.keyIndex = 0
	d.pressed = false
	d.delay = 0
}

// tick advances the demo's boot sequence: wait for the BASIC prompt, type
// the wedge shortcut, then wait for the program to land in RAM. The wedge
// auto-queues RUN once the load finishes, so nothing further is typed.
func (d *demoLoader) tick() {
	switch d.stage {
	case demoWaitForPrompt:
		if basicPrompt() {
			d.start(loadMaze)
			d.stage = demoLoading
		}
	case demoLoading:
		if d.typeKeys() {
			d.stage = demoWaitForProgram
		}
	case demoWaitForProgram:
		if mazeLoaded() {
			d.stage = demoDone
		}
	}
}

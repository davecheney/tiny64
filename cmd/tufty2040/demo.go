//go:build tinygo

package main

import (
	"bytes"

	"github.com/davecheney/tiny64"
)

// main advances the machine exactly one PAL frame per demoLoader.tick, so
// the demo's delays are counted in ticks rather than in CPU cycles: the
// caller already knows how much emulated time has passed, and asking the
// CPU for a cycle count only to divide it back down again is a counter the
// emulator would otherwise have to keep on every one of a frame's 19,656
// cycles.
//
// These are the numbers of ticks *skipped* between actions. The 40,000
// CPU cycles a key phase used to wait for is 2.03 frames, so the next key
// transition landed on the third tick; the 1,000,000 cycles of listing
// pause is 50.9 frames, so it resumed on the fifty-first.
const (
	keyPhaseFrames = 2
	listingFrames  = 50
)

type keyStroke struct {
	key   tiny64.Key
	shift bool
}

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

var loadMaze = []keyStroke{
	{key: tiny64.KeyL}, {key: tiny64.KeyO}, {key: tiny64.KeyA}, {key: tiny64.KeyD},
	{key: tiny64.Key2, shift: true},
	{key: tiny64.KeyM}, {key: tiny64.KeyA}, {key: tiny64.KeyZ}, {key: tiny64.KeyE},
	{key: tiny64.Key2, shift: true},
	{key: tiny64.KeyComma}, {key: tiny64.Key8},
	{key: tiny64.KeyReturn},
}

var runMaze = []keyStroke{
	{key: tiny64.KeyR}, {key: tiny64.KeyU}, {key: tiny64.KeyN}, {key: tiny64.KeyReturn},
}

type demoStage uint8

const (
	demoWaitForPrompt demoStage = iota
	demoLoading
	demoWaitForProgram
	demoListing
	demoWaitAfterList
	demoRunning
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
	delay    uint16 // ticks still to skip before the next action
}

func (d *demoLoader) start(keys []keyStroke) {
	d.keys = keys
	d.keyIndex = 0
	d.pressed = false
	d.delay = 0
}

func (d *demoLoader) typeKeys() bool {
	if d.delay > 0 {
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
		if mazeLoaded() && basicPrompt() {
			d.start([]keyStroke{
				{key: tiny64.KeyL},
				{key: tiny64.KeyI},
				{key: tiny64.KeyS},
				{key: tiny64.KeyT},
				{key: tiny64.KeyReturn},
			})
			d.stage = demoListing
		}
	case demoListing:
		if d.typeKeys() {
			d.delay = listingFrames
			d.stage = demoWaitAfterList
		}
	case demoWaitAfterList:
		if d.delay > 0 {
			d.delay--
		} else {
			d.start(runMaze)
			d.stage = demoRunning
		}
	case demoRunning:
		if d.typeKeys() {
			d.stage = demoDone
		}
	}
}

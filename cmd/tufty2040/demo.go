//go:build tinygo

package main

import (
	"bytes"

	"github.com/davecheney/tiny64"
)

const keyPhaseCycles = 40_000

type keyStroke struct {
	key   tiny64.Key
	shift bool
}

// mazePRG is the classic one-line Commodore 64 maze:
//
//	10 PRINT CHR$(205.5+RND(1)); : GOTO 10
//
// It prints alternating diagonal glyphs indefinitely. The program is kept as
// tokenized BASIC so it is loaded through the KERNAL rather than injected
// directly into the C64's memory.
var mazePRG = []byte{
	0x01, 0x08, // load address $0801
	0x00, 0x00, // no following BASIC line
	0x0A, 0x00, // line 10
	0x99, ' ', 0xC7, '(', '2', '0', '5', '.', '5', '+', 0xC8, '(', '1', ')', ')', ';', ' ', ':', ' ', 0x89, ' ', '1', '0',
	0x00,
}

var loadMaze = []keyStroke{
	{key: tiny64.KeyL}, {key: tiny64.KeyO}, {key: tiny64.KeyA}, {key: tiny64.KeyD},
	{key: tiny64.Key2, shift: true},
	{key: tiny64.KeyM}, {key: tiny64.KeyA}, {key: tiny64.KeyZ}, {key: tiny64.KeyE},
	{key: tiny64.Key2, shift: true},
	{key: tiny64.KeyComma}, {key: tiny64.Key8}, {key: tiny64.KeyComma}, {key: tiny64.Key1},
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
	deadline uint64
}

func (d *demoLoader) start(keys []keyStroke) {
	d.keys = keys
	d.keyIndex = 0
	d.pressed = false
	d.deadline = tiny64.GetCPU().Clock
}

func (d *demoLoader) typeKeys() bool {
	if tiny64.GetCPU().Clock < d.deadline {
		return false
	}
	if d.pressed {
		tiny64.Keys().ReleaseAll()
		d.pressed = false
		d.keyIndex++
		d.deadline = tiny64.GetCPU().Clock + keyPhaseCycles
		return d.keyIndex == len(d.keys)
	}

	key := d.keys[d.keyIndex]
	if key.shift {
		tiny64.Keys().Press(tiny64.KeyLShift)
	}
	tiny64.Keys().Press(key.key)
	d.pressed = true
	d.deadline = tiny64.GetCPU().Clock + keyPhaseCycles
	return false
}

func basicPrompt() bool {
	const prompt = 0x0400 + 5*40
	return bytes.Equal(tiny64.Ram()[prompt:prompt+6], []byte{0x12, 0x05, 0x01, 0x04, 0x19, '.'})
}

func mazeLoaded() bool {
	program := mazePRG[2:]
	return bytes.Equal(tiny64.Ram()[0x0801:0x0801+len(program)], program)
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
		if mazeLoaded() {
			d.start(runMaze)
			d.stage = demoRunning
		}
	case demoRunning:
		if d.typeKeys() {
			d.stage = demoDone
		}
	}
}

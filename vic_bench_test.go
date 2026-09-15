package tiny64

import "testing"

// loadTestProgram resets CIA1/CIA2 (Reset() doesn't touch them, and they're
// global singletons that could carry dirty state from an earlier test in
// the same process), clears RAM, then writes a tiny, safe instruction loop
// (LDA immediate, STA/INC/JMP absolute - common addressing modes, no
// illegal opcodes) and points PC at it, so benchmarks measure real
// instruction-execution cost without any risk of an interrupt firing and
// jumping through uninitialized vector bytes.
//
// It leaves DEN clear, which is worth being explicit about: with the
// screen off there are no Bad Lines, no c- or g-accesses, and
// spriteDisplay never leaves zero, so paintGraphicsPixel takes its
// display == 0 early out on every dot. That is the cheapest the machine
// ever gets. loadDisplayProgram and loadSpriteProgram below turn the
// screen and the sprites on for benchmarks that need to see that work.
func loadTestProgram() {
	cia1 = CIA{}
	cia2 = CIA{}
	vic.control1 = 0 // DEN=0: no bad lines/sprite DMA, AEC stays true
	ram := Ram()
	for i := range ram {
		ram[i] = 0
	}
	prog := []byte{
		0xA9, 0x00, // LDA #$00
		0x8D, 0x00, 0x09, // STA $0900
		0xEE, 0x00, 0x09, // INC $0900
		0x4C, 0x02, 0x08, // JMP $0802
	}
	copy(ram[0x0800:], prog)
	cpu.PC = 0x0800
}

// loadDisplayProgram is loadTestProgram with the screen on: DEN set, a
// full screen of characters and colour RAM populated, so Bad Lines,
// c-accesses and g-accesses all run and every visible dot paints a
// graphics pixel rather than border.
func loadDisplayProgram() {
	loadTestProgram()
	cia2.PRA, cia2.DDRA = 3, 3 // VIC bank 0
	vic.memPointers = 0x14     // screen $0400, chars $1000
	vic.control1 = 0x1B        // DEN=1, RSEL=1, YSCROLL=3
	vic.control2 = 0x08        // CSEL=1
	r := Ram()
	for i := range 1000 {
		r[0x0400+i] = byte(1 + i%40)
		colorRAM[i] = byte(1 + i%15)
	}
	vic.syncLineVisibility()
}

// loadSpriteProgram adds eight enabled sprites, all solid and all in the
// same 21 line band, spread across the display so the beam crosses them
// one after another - a game with a full complement of sprites on screen.
// The band is 21 of 312 raster lines, so a frame benchmark using this
// measures the sprite path at a realistic duty cycle rather than a
// worst case.
func loadSpriteProgram() {
	loadDisplayProgram()
	r := Ram()
	const shapePage = 0x30 // $0C00, clear of the program at $0800
	for i := range 8 {
		r[0x07F8+i] = shapePage
	}
	for i := range 64 {
		r[int(shapePage)*64+i] = 0xFF
	}
	for i := range 8 {
		vic.WriteRegister(uint16(0xD000+i*2), byte(24+i*24)) // X
		vic.WriteRegister(uint16(0xD001+i*2), 100)           // Y
	}
	vic.WriteRegister(0xD010, 0x00) // no X MSBs
	vic.WriteRegister(0xD015, 0xFF) // all eight enabled
}

// BenchmarkStepCycle measures the cost of one bus cycle - eight dots of
// VIC-II work plus the CPU's Phi2 - while executing a fixed, panic-free
// instruction loop, to guide where further optimization effort is worth
// spending. Desktop numbers won't match TinyGo/Cortex-M0+ absolute
// timings, but the relative proportions of work should carry over.
//
// This one runs with the screen off (see loadTestProgram), so it is a
// floor, not a representative frame. The StepFrame benchmarks below are
// what to look at when judging whether a change to the dot path paid.
func BenchmarkStepCycle(b *testing.B) {
	Reset()
	loadTestProgram()

	b.ResetTimer()
	for range b.N {
		vic.StepCycle()
	}
}

// BenchmarkTickPhi2 isolates just the CPU side (opcode fetch/execute),
// bypassing the VIC-II's per-dot bookkeeping entirely, for comparison
// against BenchmarkStepCycle.
func BenchmarkTickPhi2(b *testing.B) {
	Reset()
	loadTestProgram()

	b.ResetTimer()
	for range b.N {
		cpu.TickPhi2()
	}
}

// The three frame benchmarks below are meant to be read together: each
// adds one layer of the VIC-II's work to the one before it, so the
// difference between them is the cost of that layer over a whole frame.
// Blank is the screen off, Display adds Bad Lines and the graphics
// sequencer, and Sprites adds eight sprites for 21 of the frame's 312
// raster lines.

func BenchmarkStepFrameBlank(b *testing.B) {
	Reset()
	loadTestProgram()
	b.ResetTimer()
	for range b.N {
		vic.StepFrame()
	}
}

func BenchmarkStepFrameDisplay(b *testing.B) {
	Reset()
	loadDisplayProgram()
	b.ResetTimer()
	for range b.N {
		vic.StepFrame()
	}
}

func BenchmarkStepFrameSprites(b *testing.B) {
	Reset()
	loadSpriteProgram()
	b.ResetTimer()
	for range b.N {
		vic.StepFrame()
	}
}

// TestBenchmarkProgramsReachWhatTheyClaim guards the three setups above
// against quietly degenerating into each other. A benchmark that stops
// exercising the work it is named for does not fail; it just gets faster,
// which is indistinguishable from an optimization that worked.
func TestBenchmarkProgramsReachWhatTheyClaim(t *testing.T) {
	saveMachine(t)

	// Counts the cycles of one frame that see a Bad Line and that see a
	// sprite under DMA - the two pieces of work the setups differ by.
	observe := func(load func()) (badLine, sprite int) {
		Reset()
		load()
		for range CyclesPerFrame {
			vic.StepCycle()
			if vic.badLine {
				badLine++
			}
			if vic.spriteDisplay != 0 {
				sprite++
			}
		}
		return badLine, sprite
	}

	if badLine, sprite := observe(loadTestProgram); badLine != 0 || sprite != 0 {
		t.Errorf("loadTestProgram: %d Bad Line cycles and %d sprite cycles, want neither: DEN is clear, so this setup is the floor the others are measured against", badLine, sprite)
	}
	if badLine, sprite := observe(loadDisplayProgram); badLine == 0 || sprite != 0 {
		t.Errorf("loadDisplayProgram: %d Bad Line cycles and %d sprite cycles, want Bad Lines but no sprites", badLine, sprite)
	}
	if badLine, sprite := observe(loadSpriteProgram); badLine == 0 || sprite == 0 {
		t.Errorf("loadSpriteProgram: %d Bad Line cycles and %d sprite cycles, want both", badLine, sprite)
	}
}

package tiny64

import "testing"

// loadTestProgram resets CIA1/CIA2 (Reset() doesn't touch them, and they're
// global singletons that could carry dirty state from an earlier test in
// the same process), clears RAM, then writes a tiny, safe instruction loop
// (LDA immediate, STA/INC/JMP absolute - common addressing modes, no
// illegal opcodes) and points PC at it, so benchmarks measure real
// instruction-execution cost without any risk of an interrupt firing and
// jumping through uninitialized vector bytes.
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
	// Reset() doesn't clear the CPU's microcode state, so a benchmark run
	// that stops mid-instruction would otherwise resume at 0x0800 in a
	// non-T0 state and decode a data byte as an opcode.
	cpu.TState = 0
	cpu.Interrupt = 0
}

// BenchmarkStepDot measures the cost of VIC.StepDot() calls (which also
// drives the CPU via TickPhi2 every 8th dot) while executing a fixed,
// panic-free instruction loop, to guide where further optimization
// effort is worth spending - desktop numbers won't match TinyGo/Cortex-
// M0+ absolute timings, but the relative proportions of work should
// carry over.
func BenchmarkStepDot(b *testing.B) {
	WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	Reset()
	loadTestProgram()

	b.ResetTimer()
	for range b.N {
		vic.StepDot()
	}
}

// BenchmarkTickPhi2 isolates just the CPU side (opcode fetch/execute),
// bypassing the VIC-II's per-dot bookkeeping entirely, for comparison
// against BenchmarkStepDot.
func BenchmarkTickPhi2(b *testing.B) {
	Reset()
	loadTestProgram()

	b.ResetTimer()
	for range b.N {
		cpu.TickPhi2()
	}
}

// BenchmarkStepFrame measures the unrolled per-frame path (stepCycle and
// its eight dotclock functions), which is what the frontends actually
// call once per displayed frame.
func BenchmarkStepFrame(b *testing.B) {
	WritePixelToBuffer = func(x, y uint16, colorIndex byte) {}
	Reset()
	loadTestProgram()
	vic.control1 = 0x1B // DEN=1, RSEL=1: display enabled, normal window

	b.ResetTimer()
	for range b.N {
		vic.StepFrame()
	}
}

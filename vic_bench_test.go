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
}

// BenchmarkStepCycle measures the cost of one bus cycle - eight dots of
// VIC-II work plus the CPU's Phi2 - while executing a fixed, panic-free
// instruction loop, to guide where further optimization effort is worth
// spending. Desktop numbers won't match TinyGo/Cortex-M0+ absolute
// timings, but the relative proportions of work should carry over.
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

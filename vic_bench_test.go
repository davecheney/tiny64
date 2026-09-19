package tiny64

import "testing"

// The benchmark program lives at $0800 and its interrupt handler at $0A00,
// with two bytes of scratch at $0900 and $0902. $0314/$0315 is the KERNAL's
// indirect IRQ vector, which its $FF48 entry stub jumps through.
const (
	benchProgram = 0x0800
	benchHandler = 0x0A00
	benchScratch = 0x0900

	// The latch the KERNAL loads into CIA1 Timer A for the jiffy clock,
	// read back off a booted machine rather than quoted from memory. At
	// 16421 cycles against a frame's 19656 it fires a little over once a
	// frame.
	jiffyLatch = 0x4025
)

// loadTestProgram sets the machine up the way a C64 actually is once the
// KERNAL has finished booting, then points the CPU at a small program that
// does what C64 programs do: poll the raster, write a VIC register, and
// service interrupts.
//
// Three of those properties are here because leaving them out quietly
// zeroed the cost of whatever they exercise:
//
// CIA1 Timer A runs. It used to be left stopped (both chips were reset to
// the zero value and never started), which meant a CIA tick did nothing,
// and any change to the CIA path measured against a chip that was switched
// off. That is what made the guard added in "Split CIA.Tick so the
// stopped-timer case inlines" measure -5% here against -1.2% on the demo
// benchmarks.
//
// The program writes $D021 on every raster line. Nothing used to write a
// VIC register at all, so an optimization that moves work out of the dot
// path and onto a register write - which is the shape of several - could
// show a saving here while paying it all back on real code.
//
// Interrupts fire and are dispatched. The jiffy timer's IRQ and a raster
// IRQ are both armed and both acknowledged by the handler, so the CPU's
// interrupt sampling and dispatch path is measured rather than skipped.
//
// It leaves DEN clear, which is worth being explicit about: with the
// screen off there are no Bad Lines, no c- or g-accesses, and
// spriteDisplay never leaves zero, so paintGraphicsPixel takes its
// display == 0 early out on every dot. That is the cheapest the machine
// ever gets. loadDisplayProgram and loadSpriteProgram below turn the
// screen and the sprites on for benchmarks that need to see that work.
func loadTestProgram() {
	cia = CIA{}
	vic.control1 = 0 // DEN=0: no bad lines/sprite DMA, AEC stays true

	// These survive CPU.Reset, so a benchmark run after a test that banked
	// the ROMs out would otherwise fetch its interrupt vector from RAM.
	// Zero is the power-on state: all pins inputs, floating high, so the
	// KERNAL, BASIC and I/O are all visible.
	cpu.Port, cpu.PortDDR = 0, 0

	ram := Ram()
	for i := range ram {
		ram[i] = 0
	}

	// CLI, then poll $D012 and act once per raster line. The compare
	// against scratch keeps the loop from writing $D021 several times a
	// line, so the register write happens at a rate a raster bar would
	// rather than as fast as the CPU can store.
	copy(ram[benchProgram:], []byte{
		0x58,             // CLI
		0xAD, 0x12, 0xD0, // LDA $D012
		0xCD, 0x00, 0x09, // CMP $0900
		0xF0, 0xF8, //       BEQ -8 (back to the LDA)
		0x8D, 0x00, 0x09, //	STA $0900
		0x8D, 0x21, 0xD0, // STA $D021
		0xEE, 0x02, 0x09, // INC $0902
		0x4C, 0x01, 0x08, // JMP $0801
	})

	// The KERNAL's $FF48 stub has already pushed A, X and Y by the time it
	// reaches here, so the tail restores them the way $EA81 does. Both
	// sources are acknowledged: $D019 is write-one-to-clear, and reading
	// CIA1's ICR drops its line.
	copy(ram[benchHandler:], []byte{
		0xAD, 0x19, 0xD0, // LDA $D019
		0x8D, 0x19, 0xD0, // STA $D019
		0xAD, 0x0D, 0xDC, // LDA $DC0D
		0x68, //       PLA
		0xA8, //       TAY
		0x68, //       PLA
		0xAA, //       TAX
		0x68, //       PLA
		0x40, //       RTI
	})
	ram[0x0314] = byte(benchHandler & 0xFF)
	ram[0x0315] = byte(benchHandler >> 8)

	// CIA1 Timer A free-running off Phi2 with the jiffy latch and its
	// interrupt unmasked, which is exactly how the KERNAL leaves it.
	cia.cia1.store(0xDC04, byte(jiffyLatch&0xFF), sourceCIA1)
	cia.cia1.store(0xDC05, byte(jiffyLatch>>8), sourceCIA1)
	cia.cia1.store(0xDC0D, 0x81, sourceCIA1) // set mask, Timer A
	cia.cia1.store(0xDC0E, 0x11, sourceCIA1) // LOAD | START, continuous

	// One raster IRQ a frame, on a line inside the display window so it
	// lands among the Bad Lines rather than in the border.
	vic.WriteRegister(0xD012, 100)
	vic.WriteRegister(0xD01A, 0x01)

	cpu.PC = benchProgram
}

// loadDisplayProgram is loadTestProgram with the screen on: DEN set, a
// full screen of characters and colour RAM populated, so Bad Lines,
// c-accesses and g-accesses all run and every visible dot paints a
// graphics pixel rather than border.
func loadDisplayProgram() {
	loadTestProgram()
	cia.setVICBank(0)
	vic.memPointers = 0x14 // screen $0400, chars $1000
	vic.control1 = 0x1B    // DEN=1, RSEL=1, YSCROLL=3
	vic.control2 = 0x08    // CSEL=1
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

// The three frame benchmarks below are meant to be read together: each
// adds one layer of the VIC-II's work to the one before it, so the
// difference between them is the cost of that layer over a whole frame.
// Blank is the screen off, Display adds Bad Lines and the graphics
// sequencer, and Sprites adds eight sprites for 21 of the frame's 312
// raster lines.
//
// Read them as a layer-isolation tool, not as a verdict on whether a
// change is worth having. They run one small loop rather than a real
// program, so they answer "which layer did this land on" far better than
// they answer "how much faster is the emulator". BenchmarkDemoFrames in
// cmd/snapshot runs actual demos and is the number to quote.
//
// Blank is not a floor, despite being the least VIC work: with no Bad
// Lines the CPU runs every cycle of the frame instead of being stalled
// off the bus for 1075 of them, and an executed cycle costs more than a
// stalled one. Blank has come out above Display since before this
// program started writing $D021, and writing it widened the gap, because
// an I/O store costs more than the RAM store it replaced. Sprites minus
// Display is the clean subtraction; Blank minus Display is that plus the
// CPU cycles Bad Lines give back.

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

// TestBenchmarkProgramRunsALiveMachine guards the three properties that
// loadTestProgram sets up because leaving them out silently priced part of
// the emulator at zero: a CIA whose timers are stopped, a frame in which
// no VIC register is written, and a CPU that never takes an interrupt.
//
// Each of these fails the same quiet way the Bad Line and sprite counts
// above do. Nothing panics when the jiffy timer is stopped; the benchmark
// just stops measuring the CIA and gets faster, which reads exactly like
// an optimization that worked. One did, by a factor of four.
func TestBenchmarkProgramRunsALiveMachine(t *testing.T) {
	saveMachine(t)

	for _, tc := range []struct {
		name string
		load func()
	}{
		{"loadTestProgram", loadTestProgram},
		{"loadDisplayProgram", loadDisplayProgram},
		{"loadSpriteProgram", loadSpriteProgram},
	} {
		t.Run(tc.name, func(t *testing.T) {
			Reset()
			tc.load()

			// Let the CLI retire and the machine settle before sampling.
			for range CyclesPerFrame {
				vic.StepCycle()
			}

			background := vic.background[0]
			timerBefore := cia.cia1.timerA
			var handlerCycles, backgroundWrites int
			for range CyclesPerFrame {
				vic.StepCycle()
				if cpu.PC >= benchHandler && cpu.PC < benchHandler+16 {
					handlerCycles++
				}
				if vic.background[0] != background {
					background = vic.background[0]
					backgroundWrites++
				}
			}

			if cia.cia1.running&startA == 0 {
				t.Error("CIA1 Timer A is stopped; a CIA tick does nothing and any change to the CIA path measures against a chip that is switched off")
			}
			if cia.cia1.timerA == timerBefore {
				t.Error("CIA1 Timer A did not advance over a frame")
			}
			if handlerCycles == 0 {
				t.Error("no interrupt was dispatched over a frame; the CPU's interrupt sampling and dispatch path is not being measured")
			}
			if backgroundWrites == 0 {
				t.Error("$D021 was never written over a frame; work moved from the dot path onto a register write would look free here")
			}
			// The program acts once a raster line, so a frame should see
			// roughly one write per line. An order of magnitude either way
			// means the poll loop has stopped doing what it claims.
			if backgroundWrites < 32 {
				t.Errorf("$D021 written %d times over a frame; want roughly one per raster line", backgroundWrites)
			}
		})
	}
}

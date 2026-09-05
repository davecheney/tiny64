// Command tiny64c runs the emulator headless, with no video output, for
// testing and debugging the CPU/VIC-II without the Ebitengine graphics.
package main

import (
	"flag"
	"fmt"
	"math/rand/v2"
	"os"

	"github.com/davecheney/tiny64"
)

// kernalFuncs maps the real (non-jump-table) entry address of each standard
// KERNAL routine to its name, from https://sta.c64.org/cbm64krnfunc.html,
// so boot progress can be recognized in a trace.
var kernalFuncs = map[uint16]string{
	0xFF48: "IRQ",
	0xFF5B: "SCINIT",
	0xFDA3: "IOINIT",
	0xFD50: "RAMTAS",
	0xFD15: "RESTOR",
	0xFD1A: "VECTOR",
	0xFE18: "SETMSG",
	0xEDB9: "LSTNSA",
	0xEDC7: "TALKSA",
	0xFE25: "MEMTOP",
	0xFE34: "MEMBOT",
	0xEA87: "SCNKEY",
	0xFE21: "SETTMO",
	0xEE13: "IECIN",
	0xEDDD: "IECOUT",
	0xEDEF: "UNTALK",
	0xEDFE: "UNLSTN",
	0xED0C: "LISTEN",
	0xED09: "TALK",
	0xFE07: "READST",
	0xFE00: "SETLFS",
	0xFDF9: "SETNAM",
	0xF34A: "OPEN",
	0xF291: "CLOSE",
	0xF20E: "CHKIN",
	0xF250: "CHKOUT",
	0xF333: "CLRCHN",
	0xF157: "CHRIN",
	0xF1CA: "CHROUT",
	0xF49E: "LOAD",
	0xF5DD: "SAVE",
	0xF6E4: "SETTIM",
	0xF6DD: "RDTIM",
	0xF6ED: "STOP",
	0xF13E: "GETIN",
	0xF32F: "CLALL",
	0xF69B: "UDTIM",
	0xE505: "SCREEN",
	0xE50A: "PLOT",
	0xE500: "IOBASE",
}

func main() {
	trace := flag.Bool("trace", false, "print per-cycle CPU/bus/VIC-II state to stderr")
	cycles := flag.Int64("cycles", 0, "stop after this many CPU cycles (0 = run forever)")
	noCIAIRQ := flag.Bool("no-cia-irq", false, "prevent CIA1/CIA2 from asserting IRQ/NMI (jiffy clock/cursor blink won't work)")
	ciaTickDivisor := flag.Uint("cia-tick-divisor", 1, "only tick CIA timers once every N Phi2 cycles, to lower the IRQ frequency")
	flag.Parse()

	tiny64.DisableCIAInterrupts = *noCIAIRQ
	tiny64.CIATickDivisor = *ciaTickDivisor

	// WritePixelToBuffer must be set for the VIC-II to step, but there's no
	// display to draw to here, so just discard every pixel.
	tiny64.VIC().WritePixelToBuffer = func(x, y int, colorIndex byte) {}

	// Fill RAM with random values to simulate power-on randomness.
	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}

	tiny64.Reset()

	cpu := tiny64.GetCPU()
	bus := tiny64.GetBus()
	vic := tiny64.VIC()

	var n int64
	var stalled int64
	var lastPC uint16
	var traceAfterInterrupt int
	var lastUnderflowA uint64
	var lastUnderflowN int64
	var underflowCount int
	var lastDecimalADCCount int
	var lastCIA1IRQ bool
	for {
		vic.StepDot()

		// A CPU cycle completes once every 8 dots, right after the VIC-II's
		// phi0high hands the bus to the CPU for its Phi2 (see 6569.go).
		if vic.Dot%8 != 4 {
			continue
		}
		n++
		if !vic.AEC {
			stalled++
		}

		if irq := tiny64.CIA1().IRQ; irq != lastCIA1IRQ {
			fmt.Fprintf(os.Stderr, "%8d CIA1.IRQ -> %v (PC=%04X)\n", n, irq, cpu.PC)
			lastCIA1IRQ = irq
		}

		if bus.Address == 0xDC0D {
			rw := "W"
			if bus.RW {
				rw = "R"
			}
			fmt.Fprintf(os.Stderr, "%8d CIA1 ICR access %s data=%#02x PC=%04X\n", n, rw, bus.Data, cpu.PC)
		}

		if !bus.RW && (bus.Address == 0x0314 || bus.Address == 0x0315) {
			fmt.Fprintf(os.Stderr, "%8d WRITE $%04X = %#02x at PC=%04X\n", n, bus.Address, bus.Data, cpu.PC)
		}

		if u := tiny64.CIA1().UnderflowA; u != lastUnderflowA {
			if underflowCount < 20 {
				fmt.Fprintf(os.Stderr, "%8d CIA1 Timer A underflow #%d (delta=%d)\n", n, u, n-lastUnderflowN)
			}
			lastUnderflowA = u
			lastUnderflowN = n
			underflowCount++
		}

		if d := cpu.DecimalADCCount; d != lastDecimalADCCount {
			fmt.Fprintf(os.Stderr, "%8d ADC/SBC #%d executed in decimal mode at PC=%04X\n", n, d, cpu.LastDecimalADCPC)
			lastDecimalADCCount = d
		}

		// Print a checkpoint whenever the CPU starts executing a known
		// KERNAL routine, regardless of -trace, to track boot progress.
		if cpu.PC != lastPC {
			if name, ok := kernalFuncs[cpu.PC]; ok && cpu.TState == 1 {
				fmt.Fprintf(os.Stderr, "%8d KERNAL %-7s $%04X\n", n, name, cpu.PC)
			}
			lastPC = cpu.PC
		}

		if cpu.Opcode == 0x00 && cpu.TState == 1 && cpu.Interrupt != 0 {
			fmt.Fprintf(os.Stderr, "%8d INTERRUPT type=%d PC=%04X CIA1.IRQ=%v CIA1.IMR=%#02x CIA1.ICR=%#02x CIA1.LatchA=%d\n",
				n, cpu.Interrupt, cpu.PC, tiny64.CIA1().IRQ, tiny64.CIA1().IMR(), tiny64.CIA1().ICR(), tiny64.CIA1().LatchA())
			if traceAfterInterrupt == 0 {
				traceAfterInterrupt = 60
			}
		}
		if traceAfterInterrupt > 0 {
			fmt.Fprintf(os.Stderr, "%8d   PC=%04X OP=%02X T=%d\n", n, cpu.PC, cpu.Opcode, cpu.TState)
			traceAfterInterrupt--
		}

		if *trace {
			rw := "W"
			if bus.RW {
				rw = "R"
			}
			fmt.Fprintf(os.Stderr, "%8d PC=%04X OP=%02X T=%d A=%02X X=%02X Y=%02X SP=%02X P=%02X | ADDR=%04X DATA=%02X %s | DOT=%3d RASTER=%3d\n",
				n, cpu.PC, cpu.Opcode, cpu.TState, cpu.A, cpu.X, cpu.Y, cpu.SP, cpu.Status(),
				bus.Address, bus.Data, rw, vic.Dot, vic.RasterLine)
		}

		if *cycles > 0 && n >= *cycles {
			fmt.Fprintf(os.Stderr, "stopped after %d cycles (%d stalled, %.1f%%): PC=%04X OP=%02X T=%d A=%02X X=%02X Y=%02X SP=%02X P=%02X\n",
				n, stalled, 100*float64(stalled)/float64(n), cpu.PC, cpu.Opcode, cpu.TState, cpu.A, cpu.X, cpu.Y, cpu.SP, cpu.Status())
			return
		}
	}
}

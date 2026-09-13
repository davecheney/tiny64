// Command c64cli runs the emulator headless, with no video output, for
// testing and debugging the CPU/VIC-II without the Ebitengine graphics.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/rom"
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
	destestmax := flag.Bool("destestmax", false, "insert the DiSTestMAX MAX-mode cartridge before reset")
	deadtest := flag.Bool("deadtest", false, "insert the Dead Test MAX-mode cartridge before reset")
	disk := flag.String("disk", "", "insert this D64 disk image or PRG file into drive 8")
	prg := flag.String("prg", "", "insert this PRG file into drive 8 (formatted on a virtual disk)")
	drive := flag.String("drive", "1541", "drive to answer for device 8: \"1541\" emulates the drive's CPU and GCR, \"virtual\" implements CBM DOS directly")
	wedge := flag.Bool("wedge", false, "enable the resident DOS wedge at the BASIC prompt")
	flag.Parse()

	// Fill RAM with random values to simulate power-on randomness.
	ram := tiny64.Ram()
	for i := range ram {
		ram[i] = byte(rand.Uint())
	}
	colorRAM := tiny64.ColorRam()
	for i := range colorRAM {
		colorRAM[i] = byte(rand.Uint() & 0x0F)
	}

	if *destestmax {
		tiny64.GetBus().Insert(rom.DiagCart, true, false, true, false)
	}
	if *deadtest {
		tiny64.GetBus().Insert(rom.DeadTest, true, false, true, false)
	}
	if *wedge {
		tiny64.EnableDOSWedge()
	}
	targetFile := *disk
	if targetFile == "" {
		targetFile = *prg
	}
	if targetFile != "" {
		image, err := tiny64.ReadDiskOrPRG(os.DirFS(filepath.Dir(targetFile)), filepath.Base(targetFile))
		if err != nil {
			log.Fatal(err)
		}
		// Inserting a disk plugs a 1541 into the serial bus, if there
		// wasn't one there already.
		tiny64.InsertDisk(image)

		switch *drive {
		case "1541":
			// InsertDisk already attached one.
		case "virtual":
			// Only one device can answer for address 8, so the 1541 that
			// InsertDisk plugged in has to come out first.
			tiny64.AttachDrive(false)
			tiny64.AttachVirtualDrive(8)
		default:
			log.Fatalf("unknown -drive %q, want \"1541\" or \"virtual\"", *drive)
		}
	}

	tiny64.Reset()

	cpu := tiny64.GetCPU()
	bus := tiny64.GetBus()
	vic := tiny64.VIC()

	var n int64
	var stalled int64
	var lastPC uint16
	for {
		vic.StepDot()

		// A CPU cycle completes once every 8 dots, right after the VIC-II's
		// phi0high hands the bus to the CPU for its Phi2 (see 6569.go).
		if vic.Dot()%8 != 4 {
			continue
		}
		n++
		if !vic.AEC {
			stalled++
		}

		// Print a checkpoint whenever the CPU starts executing a known
		// KERNAL routine, regardless of -trace, to track boot progress.
		if cpu.PC != lastPC {
			if name, ok := kernalFuncs[cpu.PC]; ok && cpu.TState == 1 {
				fmt.Fprintf(os.Stderr, "%8d KERNAL %-7s $%04X\n", n, name, cpu.PC)
			}
			lastPC = cpu.PC
		}

		if !bus.RW && bus.Address >= 0xD000 && bus.Address <= 0xD3FF {
			fmt.Fprintf(os.Stderr, "%8d VIC write $%04X = %#02x at PC=%04X\n", n, bus.Address, bus.Data, cpu.PC)
		}

		if !bus.RW && bus.Address >= 0x0400 && bus.Address <= 0x07E7 {
			fmt.Fprintf(os.Stderr, "%8d SCREEN write $%04X = %#02x (%q) at PC=%04X\n", n, bus.Address, bus.Data, bus.Data, cpu.PC)
		}

		if !bus.RW && bus.Address >= 0x3800 && bus.Address <= 0x3FFF {
			fmt.Fprintf(os.Stderr, "%8d RAMCHAR write $%04X = %#02x (%q) at PC=%04X\n", n, bus.Address, bus.Data, bus.Data, cpu.PC)
		}

		if !bus.RW && bus.Address >= 0xDD00 && bus.Address <= 0xDD0F {
			fmt.Fprintf(os.Stderr, "%8d CIA2 write $%04X = %#02x at PC=%04X\n", n, bus.Address, bus.Data, cpu.PC)
		}

		if !bus.RW {
			fmt.Fprintf(os.Stderr, "%8d WRITE $%04X = %#02x at PC=%04X\n", n, bus.Address, bus.Data, cpu.PC)
		}

		// CHAREN=0 (and LORAM or HIRAM set) makes $D000-$DFFF show the
		// character ROM to the CPU instead of I/O - the classic "copy the
		// character ROM into RAM" step of redefining characters.
		if effective := (cpu.Port & cpu.PortDDR) | ^cpu.PortDDR; effective&0x04 == 0 && (effective&0x01 != 0 || effective&0x02 != 0) {
			if bus.RW && bus.Address >= 0xD000 && bus.Address <= 0xDFFF {
				fmt.Fprintf(os.Stderr, "%8d CHARROM read $%04X = %#02x at PC=%04X\n", n, bus.Address, bus.Data, cpu.PC)
			}
		}

		if *trace {
			rw := "W"
			if bus.RW {
				rw = "R"
			}
			fmt.Fprintf(os.Stderr, "%8d PC=%04X OP=%02X T=%d A=%02X X=%02X Y=%02X SP=%02X P=%02X | ADDR=%04X DATA=%02X %s | DOT=%3d RASTER=%3d\n",
				n, cpu.PC, cpu.Opcode, cpu.TState, cpu.A, cpu.X, cpu.Y, cpu.SP, cpu.Status(),
				bus.Address, bus.Data, rw, vic.Dot(), vic.RasterLine())
		}

		if *cycles > 0 && n >= *cycles {
			fmt.Fprintf(os.Stderr, "stopped after %d cycles (%d stalled, %.1f%%): PC=%04X OP=%02X T=%d A=%02X X=%02X Y=%02X SP=%02X P=%02X\n",
				n, stalled, 100*float64(stalled)/float64(n), cpu.PC, cpu.Opcode, cpu.TState, cpu.A, cpu.X, cpu.Y, cpu.SP, cpu.Status())
			return
		}
	}
}

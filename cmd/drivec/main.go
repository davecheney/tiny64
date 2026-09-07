// Command drivec runs the 1541 drive emulator headless, with an
// interactive prompt for driving the IEC serial bus by hand (standing in
// for the C64 side) and inspecting the drive's response - useful for
// testing the drive/IEC implementation before it's wired to a real C64.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/davecheney/tiny64"
)

func main() {
	tiny64.ResetDrive()

	fmt.Fprintln(os.Stderr, "drivec: 1541 drive test harness. Type 'help' for commands.")
	scanner := bufio.NewScanner(os.Stdin)
	printStatus()
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			return
		}
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "help", "?":
			printHelp()
		case "atn":
			setLine(fields, tiny64.SetCIA2ATN)
		case "clk":
			setLine(fields, tiny64.SetCIA2CLK)
		case "data":
			setLine(fields, tiny64.SetCIA2DATA)
		case "step":
			step(fields)
		case "status", "":
			printStatus()
		case "reset":
			tiny64.ResetDrive()
			printStatus()
		case "quit", "exit":
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q, type 'help'\n", fields[0])
		}
	}
}

func printHelp() {
	fmt.Fprint(os.Stderr, `commands:
  atn 0|1      set/clear the external (C64-side) ATN line
  clk 0|1      set/clear the external (C64-side) CLOCK line
  data 0|1     set/clear the external (C64-side) DATA line
  step [n]     run n drive Phi2 cycles (default 1)
  status       print PC/VIA1/bus state
  reset        reset the drive CPU
  quit         exit
`)
}

func setLine(fields []string, set func(bool)) {
	if len(fields) != 2 {
		fmt.Fprintln(os.Stderr, "usage: atn|clk|data 0|1")
		return
	}
	switch fields[1] {
	case "0":
		set(false)
	case "1":
		set(true)
	default:
		fmt.Fprintln(os.Stderr, "usage: atn|clk|data 0|1")
		return
	}
	printStatus()
}

func step(fields []string) {
	n := 1
	if len(fields) == 2 {
		var err error
		n, err = strconv.Atoi(fields[1])
		if err != nil {
			fmt.Fprintln(os.Stderr, "usage: step [n]")
			return
		}
	}
	cpu := tiny64.GetDriveCPU()
	for range n {
		cpu.TickPhi2()
	}
	printStatus()
}

func printStatus() {
	cpu := tiny64.GetDriveCPU()
	via1 := tiny64.Via1()
	fmt.Fprintf(os.Stderr, "PC=%04X OP=%02X T=%d A=%02X X=%02X Y=%02X SP=%02X | VIA1 ORB=%02X DDRB=%02X\n%s\n",
		cpu.PC, cpu.Opcode, cpu.TState, cpu.A, cpu.X, cpu.Y, cpu.SP,
		via1.ORB, via1.DDRB, tiny64.IECStatus())
}

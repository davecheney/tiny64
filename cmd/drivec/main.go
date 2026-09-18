//go:build drive1541

// Command drivec runs the 1541 drive emulator headless, with no C64 in
// the picture: it speaks the IEC serial bus itself (see iec.go), so the
// drive can be given real DOS commands - format a disk, read the error
// channel, list a directory - and the resulting D64 inspected, without
// having to boot a machine and type at a BASIC prompt.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/davecheney/tiny64"
)

// device is the drive's IEC device number; the emulated 1541's address
// jumpers are wired for #8. commandChannel is its command/error channel.
const (
	device         = 8
	commandChannel = 15
)

var (
	iec      bus
	diskPath string
)

func main() {
	tiny64.AttachDrive(true)
	tiny64.InsertDisk(tiny64.NewDisk())
	iec.idle(2_000_000) // let the DOS finish its power-on self test

	fmt.Fprintln(os.Stderr, "drivec: 1541 drive test harness. Type 'help' for commands.")
	printStatus()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		fmt.Fprint(os.Stderr, "> ")
		if !scanner.Scan() {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		cmd, rest, _ := strings.Cut(line, " ")
		if err := run(cmd, strings.TrimSpace(rest)); err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", cmd, err)
		}
	}
}

func run(cmd, rest string) error {
	switch cmd {
	case "help", "?":
		printHelp()
	case "attach":
		return attach(rest)
	case "blank":
		diskPath = ""
		tiny64.InsertDisk(tiny64.NewDisk())
		fmt.Fprintln(os.Stderr, "inserted a blank, unformatted disk")
	case "save":
		return save(rest)
	case "new":
		return newDisk(rest)
	case "cmd":
		return command(rest)
	case "status":
		return status()
	case "dir":
		return dir()
	case "idle":
		n, err := count(rest, 1_000_000)
		if err != nil {
			return err
		}
		iec.idle(n)
	case "atn":
		return setLine(rest, tiny64.SetCIA2ATN)
	case "clk":
		return setLine(rest, tiny64.SetCIA2CLK)
	case "data":
		return setLine(rest, tiny64.SetCIA2DATA)
	case "step":
		n, err := count(rest, 1)
		if err != nil {
			return err
		}
		iec.tick(n)
		printStatus()
	case "reset":
		tiny64.ResetDrive()
		iec.idle(2_000_000)
		printStatus()
	case "quit", "exit":
		os.Exit(0)
	default:
		return fmt.Errorf("unknown command, type 'help'")
	}
	return nil
}

func printHelp() {
	fmt.Fprint(os.Stderr, `commands:
  attach FILE       insert a D64 image from a file
  blank             insert a blank, unformatted disk
  save [FILE]       write the current disk image back out
  new NAME,ID       format the disk in the drive (DOS "N0:NAME,ID")
  cmd TEXT          send TEXT to the command channel
  status            read the drive's error channel
  dir               read the directory, as LOAD"$",8 and LIST would
  idle [N]          run the drive for N cycles with the bus at rest
  step [N]          run the drive for N cycles and print its state
  atn|clk|data 0|1  drive one bus line by hand
  reset             reset the drive
  quit              exit
`)
}

func attach(path string) error {
	if path == "" {
		return fmt.Errorf("usage: attach FILE")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(data) != tiny64.D64Size && len(data) != tiny64.D64Size40 {
		return fmt.Errorf("%s is %d bytes, want a %d byte (35-track) or %d byte (40-track) D64", path, len(data), tiny64.D64Size, tiny64.D64Size40)
	}
	tiny64.InsertDisk(data)
	diskPath = path
	fmt.Fprintf(os.Stderr, "inserted %s\n", path)
	return nil
}

func save(path string) error {
	if path == "" {
		path = diskPath
	}
	if path == "" {
		return fmt.Errorf("usage: save FILE")
	}
	if err := os.WriteFile(path, tiny64.DiskImage(), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "wrote %s\n", path)
	return nil
}

// newDisk formats the disk in the drive. The DOS does all the work: it
// steps the head over every track and lays each one down from scratch, so
// this exercises the whole write path rather than synthesizing an image.
func newDisk(arg string) error {
	if arg == "" {
		return fmt.Errorf("usage: new NAME,ID")
	}
	if err := command("N0:" + arg); err != nil {
		return err
	}
	// Formatting 35 tracks takes the better part of a minute of drive
	// time, with the bus idle throughout.
	iec.idle(90_000_000)
	return status()
}

func command(text string) error {
	if text == "" {
		return fmt.Errorf("usage: cmd TEXT")
	}
	return iec.command(device, commandChannel, strings.ToUpper(text))
}

func status() error {
	out, err := iec.read(device, commandChannel, 256)
	if len(out) > 0 {
		fmt.Println(strings.TrimRight(petsciiToASCII(out), "\n"))
	}
	return err
}

// dir reads the directory the same way LOAD"$",8 does - it is just a file
// named "$" that the DOS makes up on the fly - and prints it the way LIST
// would, decoding the BASIC program it is dressed up as.
func dir() error {
	if err := iec.listen(device, 0); err != nil {
		return err
	}
	if err := iec.send('$', false, true); err != nil {
		return err
	}
	if err := iec.unlisten(); err != nil {
		return err
	}

	out, err := iec.read(device, 0, 16*1024)
	if err != nil {
		return err
	}
	fmt.Print(formatDirectory(out))
	return nil
}

// formatDirectory turns the fake BASIC program the DOS sends for "$" back
// into the listing the C64 would print: a two-byte load address, then for
// each entry a link to the next, a line number that is really the block
// count, and the entry's text.
func formatDirectory(prg []byte) string {
	var b strings.Builder
	for p := 2; p+4 <= len(prg); {
		link := int(prg[p]) | int(prg[p+1])<<8
		blocks := int(prg[p+2]) | int(prg[p+3])<<8
		p += 4
		start := p
		for p < len(prg) && prg[p] != 0 {
			p++
		}
		fmt.Fprintf(&b, "%-5d%s\n", blocks, petsciiToASCII(prg[start:p]))
		p++
		if link == 0 {
			break
		}
	}
	return b.String()
}

// petsciiToASCII renders the printable part of a PETSCII string. The DOS
// writes filenames in shifted letters, which land in $C1-$DA.
func petsciiToASCII(s []byte) string {
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 0xC1 && c <= 0xDA:
			b.WriteByte(c - 0x80)
		case c == 0xA0:
			b.WriteByte(' ')
		case c == '\r':
			b.WriteByte('\n')
		case c >= 0x20 && c < 0x7F:
			b.WriteByte(c)
		default:
			b.WriteByte('.')
		}
	}
	return b.String()
}

func setLine(arg string, set func(bool)) error {
	switch arg {
	case "0":
		set(false)
	case "1":
		set(true)
	default:
		return fmt.Errorf("usage: atn|clk|data 0|1")
	}
	printStatus()
	return nil
}

func count(arg string, def int) (int, error) {
	if arg == "" {
		return def, nil
	}
	return strconv.Atoi(arg)
}

func printStatus() {
	cpu := tiny64.GetDriveCPU()
	via1 := tiny64.Via1()
	fmt.Fprintf(os.Stderr, "PC=%04X OP=%02X T=%d A=%02X X=%02X Y=%02X SP=%02X | VIA1 ORB=%02X DDRB=%02X\n%s\n",
		cpu.PC, cpu.Opcode, cpu.TState, cpu.A, cpu.X, cpu.Y, cpu.SP,
		via1.ORB, via1.DDRB, tiny64.IECStatus())
}

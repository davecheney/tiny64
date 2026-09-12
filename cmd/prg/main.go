// Command prg inspects Commodore 64 .prg files.
//
// A .prg is a two byte little endian load address followed by the bytes to
// be loaded there. With no flags prg prints a header, then decides what the
// payload is: a BASIC V2 program loaded at $0801 is listed, and if that
// program is a SYS stub with machine code behind it the machine code is
// disassembled too; anything else is disassembled from its load address.
//
//	prg game.prg                     # inspect a .prg file
//	prg -hex sprite.prg              # hex dump instead
//	prg -disasm -start '$0810' a.prg # disassemble from an address
//	prg -d64 games.d64 'ELITE*'      # a .prg stored inside a disk image
//	prg -d64 games.d64 -list         # list the disk's directory
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/disasm"
	"github.com/davecheney/tiny64/cmd/internal/prg"
)

var (
	d64Path  = flag.String("d64", "", "read the named file(s) from this D64 disk image instead of from disk files")
	list     = flag.Bool("list", false, "with -d64, list the disk directory and exit")
	asBASIC  = flag.Bool("basic", false, "decode the payload as a BASIC program")
	asDisasm = flag.Bool("disasm", false, "disassemble the payload")
	asHex    = flag.Bool("hex", false, "hex dump the payload")
	start    = flag.String("start", "", "first address to disassemble or dump (e.g. $0810, 0x0810 or 2064)")
	end      = flag.String("end", "", "last address to disassemble or dump")
	verbose  = flag.Bool("v", false, "print addresses in decimal as well as hex")
	noHeader = flag.Bool("no-header", false, "omit the header")
)

func main() {
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "prg: "+format+"\n", args...)
	}
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: prg [flags] file.prg...")
		fmt.Fprintln(os.Stderr, "       prg -d64 image.d64 [flags] NAME...")
		flag.PrintDefaults()
	}
	flag.Parse()

	if n := btoi(*asBASIC) + btoi(*asDisasm) + btoi(*asHex); n > 1 {
		log("at most one of -basic, -disasm and -hex may be given")
		os.Exit(2)
	}

	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	var disk []byte
	if *d64Path != "" {
		var err error
		if disk, err = os.ReadFile(*d64Path); err != nil {
			log("%v", err)
			os.Exit(1)
		}
		if *list {
			if err := listDisk(out, disk); err != nil {
				log("%v", err)
				os.Exit(1)
			}
			return
		}
	} else if *list {
		log("-list requires -d64")
		os.Exit(2)
	}

	names := flag.Args()
	if len(names) == 0 {
		if disk != nil {
			log("-d64 requires the name of a file on the disk")
			os.Exit(2)
		}
		// No files named: read a .prg from standard input.
		names = []string{"-"}
	}

	failed := false
	for i, name := range names {
		if len(names) > 1 {
			if i > 0 {
				fmt.Fprintln(out)
			}
			fmt.Fprintf(out, "==> %s <==\n", name)
		}
		if err := inspect(out, disk, name); err != nil {
			out.Flush()
			log("%s: %v", name, err)
			failed = true
		}
	}
	out.Flush()
	if failed {
		os.Exit(1)
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// listDisk prints the directory of a D64 image the way a C64 would.
func listDisk(w io.Writer, disk []byte) error {
	name, files, err := tiny64.D64Directory(disk)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "0 %q\n", name)
	for _, f := range files {
		splat := " "
		if !f.Closed {
			splat = "*"
		}
		locked := ""
		if f.Locked {
			locked = "<"
		}
		fmt.Fprintf(w, "%-5d%-18s%s%s%s\n", f.Blocks, `"`+f.Name+`"`, splat, f.Type, locked)
	}
	return nil
}

// load returns the bytes of the named .prg, either from a file, standard
// input, or from within a D64 image.
func load(disk []byte, name string) ([]byte, error) {
	switch {
	case disk != nil:
		return tiny64.D64ReadFile(disk, strings.ToUpper(name))
	case name == "-":
		return io.ReadAll(os.Stdin)
	default:
		return os.ReadFile(name)
	}
}

// inspect decodes and prints one .prg.
func inspect(w io.Writer, disk []byte, name string) error {
	raw, err := load(disk, name)
	if err != nil {
		return err
	}
	f, err := prg.ParseBytes(raw)
	if err != nil {
		return err
	}

	basic, isBASIC := prg.Program{}, false
	if f.LoadAddr == prg.BASICStart {
		basic, isBASIC = prg.DecodeBASIC(f.LoadAddr, f.Data)
	}

	if !*noHeader {
		printHeader(w, f, basic, isBASIC)
	}

	switch {
	case *asHex:
		data, addr, err := window(f)
		if err != nil {
			return err
		}
		return prg.Dump(w, addr, data)
	case *asDisasm:
		data, addr, err := window(f)
		if err != nil {
			return err
		}
		printDisasm(w, addr, data)
		return nil
	case *asBASIC:
		if !isBASIC {
			return fmt.Errorf("not a BASIC program (load address $%04X)", f.LoadAddr)
		}
		printBASIC(w, basic)
		return nil
	}

	// Auto mode.
	switch {
	case isBASIC:
		printBASIC(w, basic)
		// A SYS stub with bytes past the end of the BASIC program is
		// the usual shape of a machine code release: show the code,
		// starting at the SYS target when it lies in the payload, so
		// any padding between the two is skipped.
		if off, ok := codeStart(f, basic); ok {
			addr := f.LoadAddr + uint16(off)
			fmt.Fprintf(w, "\nmachine code from $%04X (SYS %d):\n", addr, basic.SysTarget)
			printDisasm(w, addr, f.Data[off:])
		}
	default:
		data, addr, err := window(f)
		if err != nil {
			return err
		}
		printDisasm(w, addr, data)
	}
	return nil
}

// codeStart returns the payload offset at which to disassemble the machine
// code behind a BASIC SYS stub, and whether there is any.
func codeStart(f *prg.File, p prg.Program) (int, bool) {
	if !p.SysOK || p.End >= len(f.Data) {
		return 0, false
	}
	off := p.End
	if sys := int(p.SysTarget) - int(f.LoadAddr); sys >= off && sys < len(f.Data) {
		off = sys
	}
	return off, true
}

func printHeader(w io.Writer, f *prg.File, basic prg.Program, isBASIC bool) {
	endAddr, wrap := f.EndAddr()
	kind := "machine code"
	if isBASIC {
		kind = "BASIC V2"
	}
	if *verbose {
		fmt.Fprintf(w, "load address:  $%04X (%d)\n", f.LoadAddr, f.LoadAddr)
		fmt.Fprintf(w, "end address:   $%04X (%d)\n", endAddr, endAddr)
		fmt.Fprintf(w, "size:          %d bytes\n", f.Size())
		fmt.Fprintf(w, "contents:      %s\n", kind)
		if isBASIC {
			fmt.Fprintf(w, "basic lines:   %d\n", len(basic.Lines))
			if basic.SysOK {
				fmt.Fprintf(w, "sys target:    $%04X (%d)\n", basic.SysTarget, basic.SysTarget)
			}
		}
	} else {
		fmt.Fprintf(w, "$%04X-$%04X  %d bytes  %s", f.LoadAddr, endAddr, f.Size(), kind)
		if isBASIC && basic.SysOK {
			fmt.Fprintf(w, ", SYS $%04X", basic.SysTarget)
		}
		fmt.Fprintln(w)
	}
	if wrap {
		fmt.Fprintln(w, "warning: payload runs past $FFFF and wraps around")
	}
	fmt.Fprintln(w)
}

func printBASIC(w io.Writer, p prg.Program) {
	for _, l := range p.Lines {
		fmt.Fprintln(w, l)
	}
}

func printDisasm(w io.Writer, addr uint16, data []byte) {
	for _, ins := range disasm.Disassemble(addr, data) {
		var raw strings.Builder
		for _, b := range ins.Bytes {
			fmt.Fprintf(&raw, "%02X ", b)
		}
		fmt.Fprintf(w, "$%04X  %-9s %s\n", ins.Addr, raw.String(), ins)
	}
}

// window applies -start and -end to a payload, returning the bytes to show
// and the address the first of them loads at. An address outside the file
// clamps to its nearest end.
func window(f *prg.File) ([]byte, uint16, error) {
	data, addr := f.Data, f.LoadAddr
	if *start != "" {
		v, err := parseAddr(*start)
		if err != nil {
			return nil, 0, err
		}
		if v > addr {
			off := int(v) - int(addr)
			if off > len(data) {
				off = len(data)
			}
			data, addr = data[off:], v
		}
	}
	if *end != "" {
		v, err := parseAddr(*end)
		if err != nil {
			return nil, 0, err
		}
		if v < addr {
			return nil, 0, fmt.Errorf("-end $%04X is before the first address $%04X", v, addr)
		}
		if n := int(v) - int(addr) + 1; n < len(data) {
			data = data[:n]
		}
	}
	return data, addr, nil
}

// parseAddr accepts an address written as $0801, 0x0801 or 2064.
func parseAddr(s string) (uint16, error) {
	s = strings.TrimSpace(s)
	base := 10
	switch {
	case strings.HasPrefix(s, "$"):
		s, base = s[1:], 16
	case strings.HasPrefix(s, "0x"), strings.HasPrefix(s, "0X"):
		s, base = s[2:], 16
	}
	v, err := strconv.ParseUint(s, base, 16)
	if err != nil {
		return 0, fmt.Errorf("bad address %q", s)
	}
	return uint16(v), nil
}

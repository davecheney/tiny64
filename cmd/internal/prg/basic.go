package prg

import (
	"fmt"
	"strconv"
	"strings"
)

// BASICStart is the load address of a program saved by C64 BASIC V2.
const BASICStart = 0x0801

// Line is one detokenised BASIC line.
type Line struct {
	Addr uint16 // address of the line's link bytes
	Num  uint16 // BASIC line number
	Text string // detokenised text, without the line number
}

// String formats the line the way LIST does.
func (l Line) String() string { return fmt.Sprintf("%d %s", l.Num, l.Text) }

// Program is a decoded BASIC program.
type Program struct {
	Lines []Line
	// End is the offset in the payload just past the program's terminating
	// link, which is where any machine code appended to a BASIC stub starts.
	End int
	// SysTarget is the address of the first SYS in the program, and SysOK
	// reports whether one was found. A one line SYS stub is the usual way a
	// machine code program is started from BASIC.
	SysTarget uint16
	SysOK     bool
}

// DecodeBASIC detokenises a BASIC V2 program held in data and loaded at
// addr. It reports ok if the line-link chain is well formed: links must
// point forward, stay inside the payload, and end with a zero link.
func DecodeBASIC(addr uint16, data []byte) (p Program, ok bool) {
	for off := 0; ; {
		if off+2 > len(data) {
			return p, false
		}
		link := uint16(data[off]) | uint16(data[off+1])<<8
		if link == 0 {
			p.End = off + 2
			return p, len(p.Lines) > 0
		}
		// The link is the address of the next line, so it must lie
		// ahead of this one and inside the payload.
		next := int(link) - int(addr)
		if next <= off || next > len(data) {
			return p, false
		}
		if off+4 > len(data) {
			return p, false
		}
		num := uint16(data[off+2]) | uint16(data[off+3])<<8
		body := data[off+4 : next]
		// The line body is terminated by a zero byte; anything after it
		// is padding the chain skips over.
		if i := indexByte(body, 0); i >= 0 {
			body = body[:i]
		}
		text := detokenise(body)
		if sys, found := sysTarget(text); found && !p.SysOK {
			p.SysTarget, p.SysOK = sys, true
		}
		p.Lines = append(p.Lines, Line{Addr: addr + uint16(off), Num: num, Text: text})
		off = next
	}
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// sysTarget returns the address argument of the first SYS in a
// detokenised line, if it has a plain decimal one.
func sysTarget(text string) (uint16, bool) {
	i := strings.Index(text, "SYS")
	if i < 0 {
		return 0, false
	}
	rest := strings.TrimLeft(text[i+len("SYS"):], " ")
	n := 0
	for n < len(rest) && rest[n] >= '0' && rest[n] <= '9' {
		n++
	}
	if n == 0 {
		return 0, false
	}
	v, err := strconv.ParseUint(rest[:n], 10, 32)
	if err != nil || v > 0xFFFF {
		return 0, false
	}
	return uint16(v), true
}

// detokenise renders one BASIC line body as text. Bytes below $80 are
// PETSCII characters, bytes from $80 are keyword tokens. Inside quotes
// nothing is a token: the quoted text is literal PETSCII.
func detokenise(body []byte) string {
	var b strings.Builder
	inQuotes := false
	for _, c := range body {
		switch {
		case c == '"':
			inQuotes = !inQuotes
			b.WriteByte('"')
		case c >= 0x80 && !inQuotes:
			if tok := tokens[c-0x80]; tok != "" {
				b.WriteString(tok)
			} else {
				fmt.Fprintf(&b, "{$%02X}", c)
			}
		default:
			b.WriteString(petscii(c))
		}
	}
	return b.String()
}

// petscii renders a single PETSCII byte as text. Printable characters are
// passed through (unshifted letters are upper case on a stock C64);
// anything else is shown as a named control code or a hex escape.
func petscii(c byte) string {
	switch {
	case c >= 0x20 && c <= 0x5F:
		return string(rune(c))
	case c >= 0x60 && c <= 0x7F, c >= 0xA0:
		// Graphics characters, which have no ASCII equivalent.
		return fmt.Sprintf("{$%02X}", c)
	}
	if name := controlNames[c]; name != "" {
		return "{" + name + "}"
	}
	return fmt.Sprintf("{$%02X}", c)
}

// controlNames are the PETSCII control codes that commonly appear inside
// quoted strings, under the names LIST shows them by.
var controlNames = map[byte]string{
	0x05: "white", 0x08: "shift disable", 0x09: "shift enable",
	0x0D: "return", 0x0E: "lower case", 0x11: "down", 0x12: "rvs on",
	0x13: "home", 0x14: "del", 0x1C: "red", 0x1D: "right",
	0x1E: "green", 0x1F: "blue",
	0x81: "orange", 0x8D: "shift return", 0x8E: "upper case",
	0x90: "black", 0x91: "up", 0x92: "rvs off", 0x93: "clr",
	0x94: "inst", 0x95: "brown", 0x96: "pink", 0x97: "dark grey",
	0x98: "grey", 0x99: "light green", 0x9A: "light blue",
	0x9B: "light grey", 0x9C: "purple", 0x9D: "left", 0x9E: "yellow",
	0x9F: "cyan",
}

// tokens are the BASIC V2 keywords, indexed by token value minus $80.
// $CC to $FE are unused; $FF is pi.
var tokens = [128]string{
	"END", "FOR", "NEXT", "DATA", "INPUT#", "INPUT", "DIM", "READ",
	"LET", "GOTO", "RUN", "IF", "RESTORE", "GOSUB", "RETURN", "REM",
	"STOP", "ON", "WAIT", "LOAD", "SAVE", "VERIFY", "DEF", "POKE",
	"PRINT#", "PRINT", "CONT", "LIST", "CLR", "CMD", "SYS", "OPEN",
	"CLOSE", "GET", "NEW", "TAB(", "TO", "FN", "SPC(", "THEN",
	"NOT", "STEP", "+", "-", "*", "/", "^", "AND",
	"OR", ">", "=", "<", "SGN", "INT", "ABS", "USR",
	"FRE", "POS", "SQR", "RND", "LOG", "EXP", "COS", "SIN",
	"TAN", "ATN", "PEEK", "LEN", "STR$", "VAL", "ASC", "CHR$",
	"LEFT$", "RIGHT$", "MID$", "GO",
	// $CC to $FE are unused and stay empty.
	0x7F: "π", // $FF
}

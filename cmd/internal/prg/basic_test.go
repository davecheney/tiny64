package prg

import (
	"testing"
)

// basicLine is one line of a program to be assembled by buildBASIC.
type basicLine struct {
	num  uint16
	body []byte
}

// buildBASIC lays out a tokenised BASIC program loaded at addr, computing
// the link to each following line the way BASIC does when a line is typed.
func buildBASIC(addr uint16, lines []basicLine) []byte {
	var out []byte
	pos := int(addr)
	for _, l := range lines {
		next := pos + 4 + len(l.body) + 1
		out = append(out, byte(next), byte(next>>8), byte(l.num), byte(l.num>>8))
		out = append(out, l.body...)
		out = append(out, 0x00)
		pos = next
	}
	return append(out, 0x00, 0x00)
}

func TestDecodeBASIC(t *testing.T) {
	tests := []struct {
		name      string
		lines     []basicLine
		want      []string
		wantSys   uint16
		wantSysOK bool
	}{
		{
			name:  "print string",
			lines: []basicLine{{10, []byte{0x99, '"', 'H', 'E', 'L', 'L', 'O', '"'}}},
			want:  []string{`10 PRINT"HELLO"`},
		},
		{
			name: "multiple lines",
			lines: []basicLine{
				{10, []byte{0x99, '"', 'H', 'I', '"'}},
				{20, []byte{0x89, '1', '0'}},
			},
			want: []string{`10 PRINT"HI"`, "20 GOTO10"},
		},
		{
			name:      "sys stub",
			lines:     []basicLine{{10, []byte{0x9E, '2', '0', '6', '4'}}},
			want:      []string{"10 SYS2064"},
			wantSys:   2064,
			wantSysOK: true,
		},
		{
			name:      "sys with space and rem",
			lines:     []basicLine{{0, []byte{0x9E, ' ', '4', '9', '1', '5', '2', ':', 0x8F, ' ', 'H', 'I'}}},
			want:      []string{"0 SYS 49152:REM HI"},
			wantSys:   49152,
			wantSysOK: true,
		},
		{
			name: "control codes are not tokens inside quotes",
			// PRINT"{clr}{down}A" - $93 and $11 stay literal.
			lines: []basicLine{{10, []byte{0x99, '"', 0x93, 0x11, 'A', '"'}}},
			want:  []string{`10 PRINT"{clr}{down}A"`},
		},
		{
			name:  "operators and functions",
			lines: []basicLine{{30, []byte{0x97, '5', '3', '2', '8', '0', ',', 0xC2, '5', '3', '2', '8', '0'}}},
			want:  []string{"30 POKE53280,PEEK53280"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildBASIC(BASICStart, tt.lines)
			p, ok := DecodeBASIC(BASICStart, data)
			if !ok {
				t.Fatalf("DecodeBASIC() ok = false, want true")
			}
			if len(p.Lines) != len(tt.want) {
				t.Fatalf("got %d lines, want %d: %v", len(p.Lines), len(tt.want), p.Lines)
			}
			for i, want := range tt.want {
				if got := p.Lines[i].String(); got != want {
					t.Errorf("line %d = %q, want %q", i, got, want)
				}
			}
			if p.SysOK != tt.wantSysOK || p.SysTarget != tt.wantSys {
				t.Errorf("SYS = $%04X, %v; want $%04X, %v", p.SysTarget, p.SysOK, tt.wantSys, tt.wantSysOK)
			}
			if p.End != len(data) {
				t.Errorf("End = %d, want %d", p.End, len(data))
			}
		})
	}
}

func TestDecodeBASICLineAddresses(t *testing.T) {
	data := buildBASIC(BASICStart, []basicLine{
		{10, []byte{0x9E, '2', '0', '6', '4'}},
		{20, []byte{0x80}},
	})
	p, ok := DecodeBASIC(BASICStart, data)
	if !ok {
		t.Fatal("DecodeBASIC() ok = false, want true")
	}
	if got, want := p.Lines[0].Addr, uint16(BASICStart); got != want {
		t.Errorf("line 0 address = $%04X, want $%04X", got, want)
	}
	if got, want := p.Lines[1].Addr, uint16(BASICStart+10); got != want {
		t.Errorf("line 1 address = $%04X, want $%04X", got, want)
	}
}

// TestDecodeBASICWithTrailingCode covers the common shape of a machine code
// release: a one line SYS stub followed by the code it starts.
func TestDecodeBASICWithTrailingCode(t *testing.T) {
	stub := buildBASIC(BASICStart, []basicLine{{10, []byte{0x9E, '2', '0', '6', '4'}}})
	code := []byte{0xA9, 0x00, 0x60}
	data := append(append([]byte{}, stub...), code...)

	p, ok := DecodeBASIC(BASICStart, data)
	if !ok {
		t.Fatal("DecodeBASIC() ok = false, want true")
	}
	if !p.SysOK || p.SysTarget != 2064 {
		t.Errorf("SYS = $%04X, %v; want $0810, true", p.SysTarget, p.SysOK)
	}
	if p.End != len(stub) {
		t.Fatalf("End = %d, want %d", p.End, len(stub))
	}
	if got := string(data[p.End:]); got != string(code) {
		t.Errorf("trailing bytes = % X, want % X", data[p.End:], code)
	}
}

func TestDecodeBASICRejectsNonBASIC(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"machine code", []byte{0xA9, 0x00, 0x8D, 0x20, 0xD0, 0x60}},
		{"backward link", []byte{0x00, 0x08, 0x0A, 0x00, 0x99, 0x00, 0x00, 0x00}},
		{"link past the end", []byte{0xFF, 0x7F, 0x0A, 0x00, 0x99, 0x00, 0x00, 0x00}},
		{"truncated: no terminator", []byte{0x0B, 0x08, 0x0A, 0x00, 0x99}},
		{"empty", nil},
		{"empty program", []byte{0x00, 0x00}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok := DecodeBASIC(BASICStart, tt.data); ok {
				t.Errorf("DecodeBASIC(% X) ok = true, want false", tt.data)
			}
		})
	}
}

package prg

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseBytes(t *testing.T) {
	tests := []struct {
		name     string
		in       []byte
		wantErr  bool
		wantAddr uint16
		wantSize int
	}{
		{name: "empty", in: nil, wantErr: true},
		{name: "one byte", in: []byte{0x01}, wantErr: true},
		{name: "address only", in: []byte{0x01, 0x08}, wantAddr: 0x0801},
		{name: "basic start", in: []byte{0x01, 0x08, 0xAA, 0xBB}, wantAddr: 0x0801, wantSize: 2},
		{name: "high load address", in: []byte{0x00, 0xC0, 0x60}, wantAddr: 0xC000, wantSize: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, err := ParseBytes(tt.in)
			if tt.wantErr {
				if !errors.Is(err, ErrShort) {
					t.Fatalf("ParseBytes(%v) error = %v, want ErrShort", tt.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseBytes(%v) = %v", tt.in, err)
			}
			if f.LoadAddr != tt.wantAddr {
				t.Errorf("LoadAddr = $%04X, want $%04X", f.LoadAddr, tt.wantAddr)
			}
			if f.Size() != tt.wantSize {
				t.Errorf("Size() = %d, want %d", f.Size(), tt.wantSize)
			}
		})
	}
}

func TestParse(t *testing.T) {
	f, err := Parse(bytes.NewReader([]byte{0x00, 0x08, 0x01, 0x02, 0x03}))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.LoadAddr != 0x0800 {
		t.Errorf("LoadAddr = $%04X, want $0800", f.LoadAddr)
	}
	if !bytes.Equal(f.Data, []byte{0x01, 0x02, 0x03}) {
		t.Errorf("Data = %v, want [1 2 3]", f.Data)
	}
}

func TestEndAddr(t *testing.T) {
	tests := []struct {
		name     string
		f        File
		wantAddr uint16
		wantWrap bool
	}{
		{"empty payload", File{LoadAddr: 0x0801}, 0x0801, false},
		{"one byte", File{LoadAddr: 0x0801, Data: make([]byte, 1)}, 0x0801, false},
		{"page", File{LoadAddr: 0xC000, Data: make([]byte, 0x100)}, 0xC0FF, false},
		{"exactly to the top", File{LoadAddr: 0xFF00, Data: make([]byte, 0x100)}, 0xFFFF, false},
		{"wraps", File{LoadAddr: 0xFFF0, Data: make([]byte, 0x20)}, 0x000F, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, wrap := tt.f.EndAddr()
			if addr != tt.wantAddr || wrap != tt.wantWrap {
				t.Errorf("EndAddr() = $%04X, %v; want $%04X, %v", addr, wrap, tt.wantAddr, tt.wantWrap)
			}
		})
	}
}

func TestDump(t *testing.T) {
	var b strings.Builder
	data := []byte{
		0x48, 0x45, 0x4C, 0x4C, 0x4F, 0x00, 0x01, 0x02,
		0xFF, 0xFE, 0x20, 0x21, 0x22, 0x23, 0x24, 0x25,
		0x41,
	}
	if err := Dump(&b, 0x0801, data); err != nil {
		t.Fatalf("Dump: %v", err)
	}
	want := "$0801  48 45 4C 4C 4F 00 01 02  FF FE 20 21 22 23 24 25  |HELLO..... !\"#$%|\n" +
		"$0811  41                                                |A|\n"
	if got := b.String(); got != want {
		t.Errorf("Dump() =\n%q\nwant\n%q", got, want)
	}
}

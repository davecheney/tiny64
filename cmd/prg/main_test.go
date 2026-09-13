package main

import (
	"strings"
	"testing"

	"github.com/davecheney/tiny64"
)

func TestParseAddr(t *testing.T) {
	tests := []struct {
		in      string
		want    uint16
		wantErr bool
	}{
		{in: "$0801", want: 0x0801},
		{in: "0x0810", want: 0x0810},
		{in: "0XC000", want: 0xC000},
		{in: "2064", want: 2064},
		{in: " $C000 ", want: 0xC000},
		{in: "$10000", wantErr: true},
		{in: "$GG", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := parseAddr(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("parseAddr(%q) error = %v, wantErr %v", tt.in, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("parseAddr(%q) = $%04X, want $%04X", tt.in, got, tt.want)
		}
	}
}

// sysStub is a .prg that loads at $0801: 10 SYS 2064, then the machine code
// at $0810 that it starts, which blacks the border and returns.
var sysStub = []byte{
	0x01, 0x08, // load address $0801
	0x0B, 0x08, 0x0A, 0x00, // link to $080B, line 10
	0x9E, '2', '0', '6', '4', 0x00, // SYS 2064
	0x00, 0x00, // end of program
	0x00, 0x00, 0x00, // padding out to $0810
	0xA9, 0x00, 0x8D, 0x20, 0xD0, 0x60, // LDA #$00; STA $D020; RTS
}

func TestInspectAuto(t *testing.T) {
	image, err := tiny64.MakeD64FromPRG("stub.prg", sysStub)
	if err != nil {
		t.Fatalf("MakeD64FromPRG: %v", err)
	}
	var b strings.Builder
	if err := inspect(&b, image, "STUB"); err != nil {
		t.Fatalf("inspect: %v", err)
	}
	got := b.String()
	for _, want := range []string{
		"$0801-$0815",             // header address range
		"21 bytes  BASIC V2",      // size and detected type
		"SYS $0810",               // stub target
		"10 SYS2064",              // the listed BASIC line
		"machine code from $0810", // the code the SYS stub starts
		"$0810  A9 00     LDA #$00",
		"$0812  8D 20 D0  STA $D020",
		"$0815  60        RTS",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

func TestListDisk(t *testing.T) {
	image, err := tiny64.MakeD64FromPRG("stub.prg", sysStub)
	if err != nil {
		t.Fatalf("MakeD64FromPRG: %v", err)
	}
	var b strings.Builder
	if err := listDisk(&b, image); err != nil {
		t.Fatalf("listDisk: %v", err)
	}
	got := b.String()
	for _, want := range []string{`0 "TINY64"`, `"STUB"`, "PRG"} {
		if !strings.Contains(got, want) {
			t.Errorf("listing missing %q:\n%s", want, got)
		}
	}
}

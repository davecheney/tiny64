package disasm

import "testing"

func TestDisassembleAddressingModes(t *testing.T) {
	tests := []struct {
		name string
		addr uint16
		data []byte
		want string
	}{
		{"implied", 0xC000, []byte{0x60}, "RTS"},
		{"accumulator", 0xC000, []byte{0x0A}, "ASL A"},
		{"immediate", 0xC000, []byte{0xA9, 0x7F}, "LDA #$7F"},
		{"zero page", 0xC000, []byte{0xA5, 0x02}, "LDA $02"},
		{"zero page,x", 0xC000, []byte{0xB5, 0x02}, "LDA $02,X"},
		{"zero page,y", 0xC000, []byte{0xB6, 0x02}, "LDX $02,Y"},
		{"indirect,x", 0xC000, []byte{0xA1, 0xFB}, "LDA ($FB,X)"},
		{"indirect,y", 0xC000, []byte{0xB1, 0xFB}, "LDA ($FB),Y"},
		{"absolute", 0xC000, []byte{0x8D, 0x20, 0xD0}, "STA $D020"},
		{"absolute,x", 0xC000, []byte{0xBD, 0x00, 0x04}, "LDA $0400,X"},
		{"absolute,y", 0xC000, []byte{0xB9, 0x00, 0x04}, "LDA $0400,Y"},
		{"indirect", 0xC000, []byte{0x6C, 0xFE, 0xFF}, "JMP ($FFFE)"},
		{"branch forward", 0xC000, []byte{0xD0, 0x02}, "BNE $C004"},
		{"branch backward", 0xC010, []byte{0xF0, 0xFC}, "BEQ $C00E"},
		{"branch across a page", 0xC0FE, []byte{0x10, 0x7F}, "BPL $C17F"},
		{"illegal", 0xC000, []byte{0x4B, 0x0F}, "*ALR #$0F"},
		{"jam", 0xC000, []byte{0x02}, "*JAM"},
		{"documented nop", 0xC000, []byte{0xEA}, "NOP"},
		{"undocumented nop", 0xC000, []byte{0x04, 0x10}, "*NOP $10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Disassemble(tt.addr, tt.data)
			if len(got) != 1 {
				t.Fatalf("got %d instructions, want 1: %v", len(got), got)
			}
			if s := got[0].String(); s != tt.want {
				t.Errorf("String() = %q, want %q", s, tt.want)
			}
			if got[0].Addr != tt.addr {
				t.Errorf("Addr = $%04X, want $%04X", got[0].Addr, tt.addr)
			}
			if len(got[0].Bytes) != len(tt.data) {
				t.Errorf("consumed %d bytes, want %d", len(got[0].Bytes), len(tt.data))
			}
		})
	}
}

func TestDisassembleTargets(t *testing.T) {
	tests := []struct {
		name       string
		addr       uint16
		data       []byte
		wantTarget uint16
		wantOK     bool
	}{
		{"branch", 0xC000, []byte{0xD0, 0xFE}, 0xC000, true},
		{"jsr", 0xC000, []byte{0x20, 0xD2, 0xFF}, 0xFFD2, true},
		{"jmp absolute", 0xC000, []byte{0x4C, 0x00, 0x08}, 0x0800, true},
		{"jmp indirect", 0xC000, []byte{0x6C, 0x00, 0x03}, 0, false},
		{"lda absolute", 0xC000, []byte{0xAD, 0x00, 0x03}, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Disassemble(tt.addr, tt.data)[0]
			if got.HasTarget != tt.wantOK || got.Target != tt.wantTarget {
				t.Errorf("target = $%04X, %v; want $%04X, %v", got.Target, got.HasTarget, tt.wantTarget, tt.wantOK)
			}
		})
	}
}

func TestDisassembleSequence(t *testing.T) {
	// The usual "border black, then return" fragment.
	code := []byte{0xA9, 0x00, 0x8D, 0x20, 0xD0, 0x60}
	want := []string{"LDA #$00", "STA $D020", "RTS"}
	got := Disassemble(0x0810, code)
	if len(got) != len(want) {
		t.Fatalf("got %d instructions, want %d", len(got), len(want))
	}
	wantAddr := []uint16{0x0810, 0x0812, 0x0815}
	for i := range want {
		if s := got[i].String(); s != want[i] {
			t.Errorf("instruction %d = %q, want %q", i, s, want[i])
		}
		if got[i].Addr != wantAddr[i] {
			t.Errorf("instruction %d address = $%04X, want $%04X", i, got[i].Addr, wantAddr[i])
		}
	}
}

func TestDisassembleTruncated(t *testing.T) {
	// A three byte instruction with only one operand byte left.
	got := Disassemble(0xC000, []byte{0x60, 0x8D, 0x20})
	if len(got) != 3 {
		t.Fatalf("got %d instructions, want 3: %v", len(got), got)
	}
	if got[0].String() != "RTS" {
		t.Errorf("instruction 0 = %q, want %q", got[0], "RTS")
	}
	for i, want := range []string{".byte $8D", ".byte $20"} {
		ins := got[i+1]
		if !ins.Truncated {
			t.Errorf("instruction %d: Truncated = false, want true", i+1)
		}
		if s := ins.String(); s != want {
			t.Errorf("instruction %d = %q, want %q", i+1, s, want)
		}
	}
}

// TestOpcodeTable checks the table is complete and self consistent: every
// byte value has a mnemonic, and every instruction's size matches its mode.
func TestOpcodeTable(t *testing.T) {
	for i := 0; i < 256; i++ {
		op := Lookup(byte(i))
		if len(op.Name) != 3 {
			t.Errorf("$%02X: name %q, want three letters", i, op.Name)
		}
		if got := Disassemble(0xC000, make([]byte, 3)[:0]); len(got) != 0 {
			t.Fatalf("empty input produced %d instructions", len(got))
		}
		data := append([]byte{byte(i)}, 0x11, 0x22)
		ins := Disassemble(0xC000, data)[0]
		if len(ins.Bytes) != op.Mode.Size() {
			t.Errorf("$%02X (%s): consumed %d bytes, want %d", i, op.Name, len(ins.Bytes), op.Mode.Size())
		}
	}
}

func TestModeSize(t *testing.T) {
	for mode, want := range map[Mode]int{
		Implied: 1, Accumulator: 1,
		Immediate: 2, ZeroPage: 2, ZeroPageX: 2, ZeroPageY: 2,
		IndirectX: 2, IndirectY: 2, Relative: 2,
		Absolute: 3, AbsoluteX: 3, AbsoluteY: 3, Indirect: 3,
	} {
		if got := mode.Size(); got != want {
			t.Errorf("Mode(%d).Size() = %d, want %d", mode, got, want)
		}
	}
}

// Package disasm implements a 6502/6510 disassembler.
//
// The opcode table covers all 256 byte values, including the undocumented
// ("illegal") instructions of the NMOS 6502 used by the C64's 6510, so no
// byte is ever reported as unknown.
package disasm

import (
	"fmt"
	"strings"
)

// Mode is a 6502 addressing mode.
type Mode int

// The 6502 addressing modes.
const (
	Implied     Mode = iota // no operand
	Accumulator             // A
	Immediate               // #$nn
	ZeroPage                // $nn
	ZeroPageX               // $nn,X
	ZeroPageY               // $nn,Y
	IndirectX               // ($nn,X)
	IndirectY               // ($nn),Y
	Absolute                // $nnnn
	AbsoluteX               // $nnnn,X
	AbsoluteY               // $nnnn,Y
	Indirect                // ($nnnn)
	Relative                // branch target
)

// Size returns the total length in bytes of an instruction using mode m,
// including its opcode.
func (m Mode) Size() int {
	switch m {
	case Implied, Accumulator:
		return 1
	case Immediate, ZeroPage, ZeroPageX, ZeroPageY, IndirectX, IndirectY, Relative:
		return 2
	default:
		return 3
	}
}

// Opcode describes a single byte value.
type Opcode struct {
	Name    string // three letter mnemonic
	Mode    Mode
	Illegal bool // true for undocumented instructions
}

// Instruction is one disassembled instruction.
type Instruction struct {
	Addr    uint16 // address of the opcode byte
	Bytes   []byte // raw bytes, including the opcode
	Op      Opcode
	Operand string // formatted operand, empty for implied
	Target  uint16 // branch or jump target, valid when HasTarget is set
	// HasTarget reports whether Target holds a meaningful address, which is
	// the case for relative branches and absolute jumps.
	HasTarget bool
	// Truncated reports that the operand bytes ran past the end of the
	// input, so the byte is emitted as raw data instead.
	Truncated bool
}

// String formats the instruction as assembler source. Illegal instructions
// are prefixed with a star.
func (i Instruction) String() string {
	if i.Truncated {
		return fmt.Sprintf(".byte $%02X", i.Bytes[0])
	}
	var b strings.Builder
	if i.Op.Illegal {
		b.WriteByte('*')
	}
	b.WriteString(i.Op.Name)
	if i.Operand != "" {
		b.WriteByte(' ')
		b.WriteString(i.Operand)
	}
	return b.String()
}

// Lookup returns the opcode table entry for the byte value op.
func Lookup(op byte) Opcode { return opcodes[op] }

// Disassemble decodes data as 6502 machine code loaded at addr. Trailing
// bytes that cannot form a complete instruction are returned as truncated
// single byte entries.
func Disassemble(addr uint16, data []byte) []Instruction {
	var out []Instruction
	for pc := 0; pc < len(data); {
		op := opcodes[data[pc]]
		size := op.Mode.Size()
		at := addr + uint16(pc)
		if pc+size > len(data) {
			out = append(out, Instruction{
				Addr:      at,
				Bytes:     data[pc : pc+1],
				Op:        op,
				Truncated: true,
			})
			pc++
			continue
		}
		ins := Instruction{
			Addr:  at,
			Bytes: data[pc : pc+size],
			Op:    op,
		}
		switch op.Mode {
		case Implied:
		case Accumulator:
			ins.Operand = "A"
		case Immediate:
			ins.Operand = fmt.Sprintf("#$%02X", data[pc+1])
		case ZeroPage:
			ins.Operand = fmt.Sprintf("$%02X", data[pc+1])
		case ZeroPageX:
			ins.Operand = fmt.Sprintf("$%02X,X", data[pc+1])
		case ZeroPageY:
			ins.Operand = fmt.Sprintf("$%02X,Y", data[pc+1])
		case IndirectX:
			ins.Operand = fmt.Sprintf("($%02X,X)", data[pc+1])
		case IndirectY:
			ins.Operand = fmt.Sprintf("($%02X),Y", data[pc+1])
		case Relative:
			// The offset is signed and relative to the address of
			// the following instruction.
			target := at + 2 + uint16(int8(data[pc+1]))
			ins.Operand = fmt.Sprintf("$%04X", target)
			ins.Target, ins.HasTarget = target, true
		default:
			word := uint16(data[pc+1]) | uint16(data[pc+2])<<8
			switch op.Mode {
			case Absolute:
				ins.Operand = fmt.Sprintf("$%04X", word)
				if op.Name == "JMP" || op.Name == "JSR" {
					ins.Target, ins.HasTarget = word, true
				}
			case AbsoluteX:
				ins.Operand = fmt.Sprintf("$%04X,X", word)
			case AbsoluteY:
				ins.Operand = fmt.Sprintf("$%04X,Y", word)
			case Indirect:
				ins.Operand = fmt.Sprintf("($%04X)", word)
			}
		}
		out = append(out, ins)
		pc += size
	}
	return out
}

// opcodes is the full 6502 opcode matrix, row by row from $00 to $FF.
var opcodes = [256]Opcode{
	// $00
	{"BRK", Implied, false}, {"ORA", IndirectX, false}, {"JAM", Implied, true}, {"SLO", IndirectX, true},
	{"NOP", ZeroPage, true}, {"ORA", ZeroPage, false}, {"ASL", ZeroPage, false}, {"SLO", ZeroPage, true},
	{"PHP", Implied, false}, {"ORA", Immediate, false}, {"ASL", Accumulator, false}, {"ANC", Immediate, true},
	{"NOP", Absolute, true}, {"ORA", Absolute, false}, {"ASL", Absolute, false}, {"SLO", Absolute, true},
	// $10
	{"BPL", Relative, false}, {"ORA", IndirectY, false}, {"JAM", Implied, true}, {"SLO", IndirectY, true},
	{"NOP", ZeroPageX, true}, {"ORA", ZeroPageX, false}, {"ASL", ZeroPageX, false}, {"SLO", ZeroPageX, true},
	{"CLC", Implied, false}, {"ORA", AbsoluteY, false}, {"NOP", Implied, true}, {"SLO", AbsoluteY, true},
	{"NOP", AbsoluteX, true}, {"ORA", AbsoluteX, false}, {"ASL", AbsoluteX, false}, {"SLO", AbsoluteX, true},
	// $20
	{"JSR", Absolute, false}, {"AND", IndirectX, false}, {"JAM", Implied, true}, {"RLA", IndirectX, true},
	{"BIT", ZeroPage, false}, {"AND", ZeroPage, false}, {"ROL", ZeroPage, false}, {"RLA", ZeroPage, true},
	{"PLP", Implied, false}, {"AND", Immediate, false}, {"ROL", Accumulator, false}, {"ANC", Immediate, true},
	{"BIT", Absolute, false}, {"AND", Absolute, false}, {"ROL", Absolute, false}, {"RLA", Absolute, true},
	// $30
	{"BMI", Relative, false}, {"AND", IndirectY, false}, {"JAM", Implied, true}, {"RLA", IndirectY, true},
	{"NOP", ZeroPageX, true}, {"AND", ZeroPageX, false}, {"ROL", ZeroPageX, false}, {"RLA", ZeroPageX, true},
	{"SEC", Implied, false}, {"AND", AbsoluteY, false}, {"NOP", Implied, true}, {"RLA", AbsoluteY, true},
	{"NOP", AbsoluteX, true}, {"AND", AbsoluteX, false}, {"ROL", AbsoluteX, false}, {"RLA", AbsoluteX, true},
	// $40
	{"RTI", Implied, false}, {"EOR", IndirectX, false}, {"JAM", Implied, true}, {"SRE", IndirectX, true},
	{"NOP", ZeroPage, true}, {"EOR", ZeroPage, false}, {"LSR", ZeroPage, false}, {"SRE", ZeroPage, true},
	{"PHA", Implied, false}, {"EOR", Immediate, false}, {"LSR", Accumulator, false}, {"ALR", Immediate, true},
	{"JMP", Absolute, false}, {"EOR", Absolute, false}, {"LSR", Absolute, false}, {"SRE", Absolute, true},
	// $50
	{"BVC", Relative, false}, {"EOR", IndirectY, false}, {"JAM", Implied, true}, {"SRE", IndirectY, true},
	{"NOP", ZeroPageX, true}, {"EOR", ZeroPageX, false}, {"LSR", ZeroPageX, false}, {"SRE", ZeroPageX, true},
	{"CLI", Implied, false}, {"EOR", AbsoluteY, false}, {"NOP", Implied, true}, {"SRE", AbsoluteY, true},
	{"NOP", AbsoluteX, true}, {"EOR", AbsoluteX, false}, {"LSR", AbsoluteX, false}, {"SRE", AbsoluteX, true},
	// $60
	{"RTS", Implied, false}, {"ADC", IndirectX, false}, {"JAM", Implied, true}, {"RRA", IndirectX, true},
	{"NOP", ZeroPage, true}, {"ADC", ZeroPage, false}, {"ROR", ZeroPage, false}, {"RRA", ZeroPage, true},
	{"PLA", Implied, false}, {"ADC", Immediate, false}, {"ROR", Accumulator, false}, {"ARR", Immediate, true},
	{"JMP", Indirect, false}, {"ADC", Absolute, false}, {"ROR", Absolute, false}, {"RRA", Absolute, true},
	// $70
	{"BVS", Relative, false}, {"ADC", IndirectY, false}, {"JAM", Implied, true}, {"RRA", IndirectY, true},
	{"NOP", ZeroPageX, true}, {"ADC", ZeroPageX, false}, {"ROR", ZeroPageX, false}, {"RRA", ZeroPageX, true},
	{"SEI", Implied, false}, {"ADC", AbsoluteY, false}, {"NOP", Implied, true}, {"RRA", AbsoluteY, true},
	{"NOP", AbsoluteX, true}, {"ADC", AbsoluteX, false}, {"ROR", AbsoluteX, false}, {"RRA", AbsoluteX, true},
	// $80
	{"NOP", Immediate, true}, {"STA", IndirectX, false}, {"NOP", Immediate, true}, {"SAX", IndirectX, true},
	{"STY", ZeroPage, false}, {"STA", ZeroPage, false}, {"STX", ZeroPage, false}, {"SAX", ZeroPage, true},
	{"DEY", Implied, false}, {"NOP", Immediate, true}, {"TXA", Implied, false}, {"ANE", Immediate, true},
	{"STY", Absolute, false}, {"STA", Absolute, false}, {"STX", Absolute, false}, {"SAX", Absolute, true},
	// $90
	{"BCC", Relative, false}, {"STA", IndirectY, false}, {"JAM", Implied, true}, {"SHA", IndirectY, true},
	{"STY", ZeroPageX, false}, {"STA", ZeroPageX, false}, {"STX", ZeroPageY, false}, {"SAX", ZeroPageY, true},
	{"TYA", Implied, false}, {"STA", AbsoluteY, false}, {"TXS", Implied, false}, {"TAS", AbsoluteY, true},
	{"SHY", AbsoluteX, true}, {"STA", AbsoluteX, false}, {"SHX", AbsoluteY, true}, {"SHA", AbsoluteY, true},
	// $A0
	{"LDY", Immediate, false}, {"LDA", IndirectX, false}, {"LDX", Immediate, false}, {"LAX", IndirectX, true},
	{"LDY", ZeroPage, false}, {"LDA", ZeroPage, false}, {"LDX", ZeroPage, false}, {"LAX", ZeroPage, true},
	{"TAY", Implied, false}, {"LDA", Immediate, false}, {"TAX", Implied, false}, {"LXA", Immediate, true},
	{"LDY", Absolute, false}, {"LDA", Absolute, false}, {"LDX", Absolute, false}, {"LAX", Absolute, true},
	// $B0
	{"BCS", Relative, false}, {"LDA", IndirectY, false}, {"JAM", Implied, true}, {"LAX", IndirectY, true},
	{"LDY", ZeroPageX, false}, {"LDA", ZeroPageX, false}, {"LDX", ZeroPageY, false}, {"LAX", ZeroPageY, true},
	{"CLV", Implied, false}, {"LDA", AbsoluteY, false}, {"TSX", Implied, false}, {"LAS", AbsoluteY, true},
	{"LDY", AbsoluteX, false}, {"LDA", AbsoluteX, false}, {"LDX", AbsoluteY, false}, {"LAX", AbsoluteY, true},
	// $C0
	{"CPY", Immediate, false}, {"CMP", IndirectX, false}, {"NOP", Immediate, true}, {"DCP", IndirectX, true},
	{"CPY", ZeroPage, false}, {"CMP", ZeroPage, false}, {"DEC", ZeroPage, false}, {"DCP", ZeroPage, true},
	{"INY", Implied, false}, {"CMP", Immediate, false}, {"DEX", Implied, false}, {"SBX", Immediate, true},
	{"CPY", Absolute, false}, {"CMP", Absolute, false}, {"DEC", Absolute, false}, {"DCP", Absolute, true},
	// $D0
	{"BNE", Relative, false}, {"CMP", IndirectY, false}, {"JAM", Implied, true}, {"DCP", IndirectY, true},
	{"NOP", ZeroPageX, true}, {"CMP", ZeroPageX, false}, {"DEC", ZeroPageX, false}, {"DCP", ZeroPageX, true},
	{"CLD", Implied, false}, {"CMP", AbsoluteY, false}, {"NOP", Implied, true}, {"DCP", AbsoluteY, true},
	{"NOP", AbsoluteX, true}, {"CMP", AbsoluteX, false}, {"DEC", AbsoluteX, false}, {"DCP", AbsoluteX, true},
	// $E0
	{"CPX", Immediate, false}, {"SBC", IndirectX, false}, {"NOP", Immediate, true}, {"ISC", IndirectX, true},
	{"CPX", ZeroPage, false}, {"SBC", ZeroPage, false}, {"INC", ZeroPage, false}, {"ISC", ZeroPage, true},
	{"INX", Implied, false}, {"SBC", Immediate, false}, {"NOP", Implied, false}, {"SBC", Immediate, true},
	{"CPX", Absolute, false}, {"SBC", Absolute, false}, {"INC", Absolute, false}, {"ISC", Absolute, true},
	// $F0
	{"BEQ", Relative, false}, {"SBC", IndirectY, false}, {"JAM", Implied, true}, {"ISC", IndirectY, true},
	{"NOP", ZeroPageX, true}, {"SBC", ZeroPageX, false}, {"INC", ZeroPageX, false}, {"ISC", ZeroPageX, true},
	{"SED", Implied, false}, {"SBC", AbsoluteY, false}, {"NOP", Implied, true}, {"ISC", AbsoluteY, true},
	{"NOP", AbsoluteX, true}, {"SBC", AbsoluteX, false}, {"INC", AbsoluteX, false}, {"ISC", AbsoluteX, true},
}

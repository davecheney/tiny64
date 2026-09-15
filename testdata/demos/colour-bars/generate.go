//go:build ignore

// Run from this directory with: go run generate.go
package main

import (
	"log"
	"os"
)

func main() {
	program := []byte{
		0x01, 0x08, // PRG load address $0801
		0x0b, 0x08, 0x0a, 0x00, 0x9e, '2', '0', '6', '1', 0x00, // 10 SYS2061
		0x00, 0x00, // end of BASIC program
		0x78,       // $080d: SEI
		0xa9, 0x06, // LDA #6
		0x8d, 0x20, 0xd0, // STA $d020 (blue border)
		0xa9, 0x00, // LDA #0
		0x8d, 0x21, 0xd0, // STA $d021 (black background)
		0xa2, 0x00, // LDX #0
	}
	loop := len(program)
	program = append(program,
		0x8a,       // TXA
		0x29, 0x0f, // AND #15
		0x9d, 0x00, 0xd8, // STA $d800,X
		0x9d, 0x00, 0xd9, // STA $d900,X
		0x9d, 0x00, 0xda, // STA $da00,X
		0x9d, 0x00, 0xdb, // STA $db00,X
		0xa9, 0xa0, // LDA #$a0 (reverse space: solid character)
		0x9d, 0x00, 0x04, // STA $0400,X
		0x9d, 0x00, 0x05, // STA $0500,X
		0x9d, 0x00, 0x06, // STA $0600,X
		0x9d, 0x00, 0x07, // STA $0700,X
		0xe8, // INX
	)
	program = append(program, 0xd0, byte(loop-(len(program)+2))) // BNE loop
	halt := 0x0801 + len(program) - 2
	program = append(program, 0x4c, byte(halt), byte(halt>>8)) // JMP self
	if err := os.WriteFile("colour-bars.prg", program, 0o644); err != nil {
		log.Fatal(err)
	}
}

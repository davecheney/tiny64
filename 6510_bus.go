package tiny64

// cpuWritesThisCycle decodes only the writing instruction families. It is
// needed only while BA is low. IRQ/NMI entry uses BRK's opcode; T0 always
// reads even though opcode still names the preceding instruction.
func cpuWritesThisCycle(opcode, tstate uint8) bool {
	if opcode&0xC0 != 0x80 && opcode&7 == 6 {
		// Memory shifts/rotates and INC/DEC: absolute addressing and
		// indexing each add a cycle before the two RMW writes.
		first := uint8(3) + (opcode>>3)&1 + (opcode>>4)&1
		return tstate == first || tstate == first+1
	}
	if opcode == 0xC7 { // Implemented DCP zero-page.
		return tstate == 3 || tstate == 4
	}
	if opcode == 0x00 {
		return tstate >= 2 && tstate <= 4
	}
	if opcode == 0x20 {
		return tstate == 3 || tstate == 4
	}
	if opcode == 0x08 || opcode == 0x48 {
		return tstate == 2
	}
	if opcode >= 0x84 && opcode <= 0x86 {
		return tstate == 2
	}
	if (opcode >= 0x8C && opcode <= 0x8E) || (opcode >= 0x94 && opcode <= 0x96) {
		return tstate == 3
	}
	if opcode == 0x99 || opcode == 0x9D {
		return tstate == 4
	}
	return (opcode == 0x81 || opcode == 0x91) && tstate == 5
}

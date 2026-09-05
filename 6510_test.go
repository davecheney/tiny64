package tiny64

import "testing"

// busCycle describes one expected Phi2 bus transaction: the address
// asserted, the byte transferred, and the direction (read vs write).
type busCycle struct {
	Addr  uint16
	Data  uint8
	Write bool // false = read, true = write
}

// cpuState captures the externally visible CPU register state, used for
// both the initial setup and the expected post-execution assertions.
type cpuState struct {
	PC      uint16
	A, X, Y uint8
	SP      uint8
	Status  uint8
}

// instrTest is a single self-contained instruction test: an initial
// machine state, the bus cycles it is expected to generate (one entry
// per TickPhi2 call), and the resulting CPU/RAM state.
type instrTest struct {
	name string

	initial cpuState
	ram     map[uint16]uint8 // sparse initial memory contents

	cycles []busCycle // expected bus activity, in order

	final    cpuState
	finalRAM map[uint16]uint8 // memory locations to spot-check afterwards
}

func runInstrTest(t *testing.T, tc instrTest) {
	t.Helper()

	// Reset all global state so tests don't leak into one another.
	ram = [65536]byte{}
	bus = Bus{}
	cpu = CPU{}
	cpu.PortDDR = 0xFF // LORAM/HIRAM/CHAREN driven low: PLA maps plain RAM everywhere

	cpu.PC = tc.initial.PC
	cpu.A = tc.initial.A
	cpu.X = tc.initial.X
	cpu.Y = tc.initial.Y
	cpu.SP = tc.initial.SP
	cpu.regP = tc.initial.Status

	for addr, val := range tc.ram {
		ram[addr] = val
	}

	vic.BA = true
	vic.AEC = true

	got := make([]busCycle, len(tc.cycles))
	for i := range tc.cycles {
		cpu.TickPhi2()
		got[i] = busCycle{Addr: bus.Address, Data: bus.Data, Write: !bus.RW}
	}

	for i, want := range tc.cycles {
		if got[i] != want {
			t.Errorf("cycle %d: got %+v, want %+v", i, got[i], want)
		}
	}

	if cpu.PC != tc.final.PC {
		t.Errorf("PC: got %04X, want %04X", cpu.PC, tc.final.PC)
	}
	if cpu.A != tc.final.A {
		t.Errorf("A: got %02X, want %02X", cpu.A, tc.final.A)
	}
	if cpu.X != tc.final.X {
		t.Errorf("X: got %02X, want %02X", cpu.X, tc.final.X)
	}
	if cpu.Y != tc.final.Y {
		t.Errorf("Y: got %02X, want %02X", cpu.Y, tc.final.Y)
	}
	if cpu.SP != tc.final.SP {
		t.Errorf("SP: got %02X, want %02X", cpu.SP, tc.final.SP)
	}
	if cpu.regP != tc.final.Status {
		t.Errorf("Status: got %02X, want %02X", cpu.regP, tc.final.Status)
	}

	for addr, want := range tc.finalRAM {
		if got := ram[addr]; got != want {
			t.Errorf("RAM[%04X]: got %02X, want %02X", addr, got, want)
		}
	}
}

// instrTests is the full table of instruction-level test cases. Add new
// cases here rather than writing standalone Test functions.
// sort by opcode, then by addressing mode, then by mnemonic.
var instrTests = []instrTest{
	{
		name:    "BRK",
		initial: cpuState{PC: 0x0200, SP: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x00, // BRK
			0x0201: 0xAA, // signature byte, read but discarded
			0xFFFE: 0x00, // IRQ/BRK vector low byte
			0xFFFF: 0xA0, // IRQ/BRK vector high byte
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x00, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xAA, Write: false}, // T1: signature byte, discarded
			{Addr: 0x01FF, Data: 0x02, Write: true},  // T2: push PCH
			{Addr: 0x01FE, Data: 0x02, Write: true},  // T3: push PCL
			{Addr: 0x01FD, Data: 0x30, Write: true},  // T4: push P with B and unused bits set
			{Addr: 0xFFFE, Data: 0x00, Write: false}, // T5: fetch new PCL from vector
			{Addr: 0xFFFF, Data: 0xA0, Write: false}, // T6: fetch new PCH from vector
		},
		final: cpuState{PC: 0xA000, SP: 0xFC, Status: 0x04}, // I flag set
		finalRAM: map[uint16]uint8{
			0x01FF: 0x02,
			0x01FE: 0x02,
			0x01FD: 0x30,
		},
	},
	{
		name:    "ORA ($20,X)",
		initial: cpuState{PC: 0x0200, A: 0x0F, X: 0x04},
		ram: map[uint16]uint8{
			0x0200: 0x01, // ORA (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
			0x0300: 0xF0, // operand ORed into A
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x01, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0xF0, Write: false}, // T5: read operand, OR with A
		},
		final: cpuState{PC: 0x0202, A: 0xFF, X: 0x04, Status: 0x80}, // N flag set
	},
	{
		name:    "ORA $10 ($05)",
		initial: cpuState{PC: 0x0200, A: 0x0F},
		ram: map[uint16]uint8{
			0x0200: 0x05, // ORA zp
			0x0201: 0x10,
			0x0010: 0xF0,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x05, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0xF0, Write: false}, // T2: read operand, OR with A
		},
		final: cpuState{PC: 0x0202, A: 0xFF, Status: 0x80}, // N flag set
	},
	{
		name:    "ASL $10 ($06)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0x06, // ASL zp
			0x0201: 0x10,
			0x0010: 0x81, // 10000001, bit 7 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x06, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x81, Write: false}, // T2: read old value
			{Addr: 0x0010, Data: 0x81, Write: true},  // T3: dummy write-back of old value
			{Addr: 0x0010, Data: 0x02, Write: true},  // T4: write shifted value
		},
		final:    cpuState{PC: 0x0202, Status: 0x01}, // C flag set from old bit 7
		finalRAM: map[uint16]uint8{0x0010: 0x02},
	},
	{
		name:    "PHP ($08)",
		initial: cpuState{PC: 0x0200, SP: 0xFF, Status: 0x81},
		ram: map[uint16]uint8{
			0x0200: 0x08, // PHP
			0x0201: 0x77, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x08, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x77, Write: false}, // T1: dummy read, no PC advance
			{Addr: 0x01FF, Data: 0xB1, Write: true},  // T2: push status, B and unused bits set
		},
		final:    cpuState{PC: 0x0201, SP: 0xFE, Status: 0x81},
		finalRAM: map[uint16]uint8{0x01FF: 0xB1},
	},
	{
		name:    "ORA #$F0 ($09)",
		initial: cpuState{PC: 0x0200, A: 0x0F},
		ram: map[uint16]uint8{
			0x0200: 0x09, // ORA #
			0x0201: 0xF0,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x09, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xF0, Write: false}, // T1: operand fetch, OR with A
		},
		final: cpuState{PC: 0x0202, A: 0xFF, Status: 0x80}, // N flag set
	},
	{
		name:    "ASL A ($0A)",
		initial: cpuState{PC: 0x0200, A: 0x81}, // 10000001, bit 7 set
		ram: map[uint16]uint8{
			0x0200: 0x0A, // ASL A
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x0A, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x02, Status: 0x01}, // C flag set from old bit 7
	},
	{
		name:    "ORA $1234 ($0D)",
		initial: cpuState{PC: 0x0200, A: 0x0F},
		ram: map[uint16]uint8{
			0x0200: 0x0D, // ORA abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0xF0,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x0D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0xF0, Write: false}, // T3: read operand, OR with A
		},
		final: cpuState{PC: 0x0203, A: 0xFF, Status: 0x80}, // N flag set
	},
	{
		name:    "ASL $1234 ($0E)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0x0E, // ASL abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x40, // 01000000, bit 7 clear
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x0E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x40, Write: false}, // T3: read old value
			{Addr: 0x1234, Data: 0x40, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x1234, Data: 0x80, Write: true},  // T5: write shifted value
		},
		final:    cpuState{PC: 0x0203, Status: 0x80}, // N flag set, C clear
		finalRAM: map[uint16]uint8{0x1234: 0x80},
	},
	{
		name:    "BPL not taken ($10)",
		initial: cpuState{PC: 0x0200, Status: 0x80}, // N set: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0x10, // BPL
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x10, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202, Status: 0x80},
	},
	{
		name:    "BPL taken, no page cross ($10)",
		initial: cpuState{PC: 0x0200}, // N clear: branch taken
		ram: map[uint16]uint8{
			0x0200: 0x10, // BPL
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x10, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207},
	},
	{
		name:    "BPL taken, page cross ($10)",
		initial: cpuState{PC: 0x02EE}, // N clear: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0x10, // BPL
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0x10, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310},
	},
	{
		name:    "CLC ($18)",
		initial: cpuState{PC: 0x0200, Status: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x18, // CLC
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x18, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Status: 0xFE}, // Only Carry cleared
	},
	{
		name:    "ORA ($20),Y no page cross ($11)",
		initial: cpuState{PC: 0x0200, A: 0x0F, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x11, // ORA (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0x00, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $0300
			0x0301: 0xF0, // operand ORed into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x11, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0x00, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0301, Data: 0xF0, Write: false}, // T4: read operand, OR with A
		},
		final: cpuState{PC: 0x0202, A: 0xFF, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "ORA ($20),Y page cross ($11)",
		initial: cpuState{PC: 0x0200, A: 0x0F, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x11, // ORA (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0xF0, // operand ORed into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x11, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T4: dummy read at wrong address
			{Addr: 0x0400, Data: 0xF0, Write: false}, // T5: read operand at fixed address, OR with A
		},
		final: cpuState{PC: 0x0202, A: 0xFF, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "ORA $10,X ($15)",
		initial: cpuState{PC: 0x0200, A: 0x0F, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x15, // ORA zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0xF0, // operand ORed into A, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x15, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0xF0, Write: false}, // T3: read operand, OR with A
		},
		final: cpuState{PC: 0x0202, A: 0xFF, X: 0x05, Status: 0x80}, // N flag set
	},
	{
		name:    "ASL $10,X ($16)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x16, // ASL zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x81, // old value at BAL+X, bit 7 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x16, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x81, Write: false}, // T3: read old value
			{Addr: 0x0015, Data: 0x81, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x0015, Data: 0x02, Write: true},  // T5: write shifted value
		},
		final:    cpuState{PC: 0x0202, X: 0x05, Status: 0x01}, // C flag set from old bit 7
		finalRAM: map[uint16]uint8{0x0015: 0x02},
	},
	{
		name:    "ORA $02FF,Y no page cross ($19)",
		initial: cpuState{PC: 0x0200, A: 0x0F, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x19, // ORA abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0xF0, // operand ORed into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x19, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0xF0, Write: false}, // T3: read operand, OR with A
		},
		final: cpuState{PC: 0x0203, A: 0xFF, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "ORA $03FF,Y page cross ($19)",
		initial: cpuState{PC: 0x0200, A: 0x0F, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x19, // ORA abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0xF0, // operand ORed into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x19, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0xF0, Write: false}, // T4: read operand at fixed address, OR with A
		},
		final: cpuState{PC: 0x0203, A: 0xFF, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "ORA $02FF,X no page cross ($1D)",
		initial: cpuState{PC: 0x0200, A: 0x0F, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x1D, // ORA abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0xF0, // operand ORed into A, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x1D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0xF0, Write: false}, // T3: read operand, OR with A
		},
		final: cpuState{PC: 0x0203, A: 0xFF, X: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "ORA $03FF,X page cross ($1D)",
		initial: cpuState{PC: 0x0200, A: 0x0F, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x1D, // ORA abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0xF0, // operand ORed into A, at corrected address base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x1D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0xF0, Write: false}, // T4: read operand at fixed address, OR with A
		},
		final: cpuState{PC: 0x0203, A: 0xFF, X: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "ASL $02FF,X ($1E)",
		initial: cpuState{PC: 0x0200, X: 0x02},
		ram: map[uint16]uint8{
			0x0200: 0x1E, // ASL abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
			0x0301: 0x81, // old value at base+X, bit 7 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x1E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T3: dummy read at wrong address (reuses byte at $0201)
			{Addr: 0x0301, Data: 0x81, Write: false}, // T4: read old value at fixed address
			{Addr: 0x0301, Data: 0x81, Write: true},  // T5: dummy write-back of old value
			{Addr: 0x0301, Data: 0x02, Write: true},  // T6: write shifted value
		},
		final:    cpuState{PC: 0x0203, X: 0x02, Status: 0x01}, // C flag set from old bit 7
		finalRAM: map[uint16]uint8{0x0301: 0x02},
	},
	{
		name:    "JSR $1234 ($20)",
		initial: cpuState{PC: 0x0200, SP: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x20, // JSR
			0x0201: 0x34, // target address low byte
			0x0202: 0x12, // target address high byte
			0x01FF: 0x00, // dummy value under current SP, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x20, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch target address low byte
			{Addr: 0x01FF, Data: 0x00, Write: false}, // T2: internal dummy read of stack
			{Addr: 0x01FF, Data: 0x02, Write: true},  // T3: push PCH (of return address - 1)
			{Addr: 0x01FE, Data: 0x02, Write: true},  // T4: push PCL (of return address - 1)
			{Addr: 0x0202, Data: 0x12, Write: false}, // T5: fetch target address high byte
		},
		final: cpuState{PC: 0x1234, SP: 0xFD},
		finalRAM: map[uint16]uint8{
			0x01FF: 0x02,
			0x01FE: 0x02,
		},
	},
	{
		name:    "AND ($20,X) ($21)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x04},
		ram: map[uint16]uint8{
			0x0200: 0x21, // AND (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
			0x0300: 0x0F, // operand ANDed into A
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x21, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0x0F, Write: false}, // T5: read operand, AND with A
		},
		final: cpuState{PC: 0x0202, A: 0x0F, X: 0x04, Status: 0x00},
	},
	{
		name:    "BIT $10 ($24)",
		initial: cpuState{PC: 0x0200, A: 0x0F},
		ram: map[uint16]uint8{
			0x0200: 0x24, // BIT zp
			0x0201: 0x10,
			0x0010: 0xC0, // 11000000: N and V set; A&M == 0 sets Z
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x24, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0xC0, Write: false}, // T2: read operand, update N/V/Z
		},
		final: cpuState{PC: 0x0202, A: 0x0F, Status: 0xC2}, // N, V, Z flags set
	},
	{
		name:    "AND $10 ($25)",
		initial: cpuState{PC: 0x0200, A: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x25, // AND zp
			0x0201: 0x10,
			0x0010: 0x0F,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x25, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x0F, Write: false}, // T2: read operand, AND with A
		},
		final: cpuState{PC: 0x0202, A: 0x0F, Status: 0x00},
	},
	{
		name:    "ROL $10 ($26)",
		initial: cpuState{PC: 0x0200, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x26, // ROL zp
			0x0201: 0x10,
			0x0010: 0x81, // 10000001, bit 7 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x26, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x81, Write: false}, // T2: read old value
			{Addr: 0x0010, Data: 0x81, Write: true},  // T3: dummy write-back of old value
			{Addr: 0x0010, Data: 0x03, Write: true},  // T4: write rotated value
		},
		final:    cpuState{PC: 0x0202, Status: 0x01}, // C flag set from old bit 7
		finalRAM: map[uint16]uint8{0x0010: 0x03},
	},
	{
		name:    "PLP ($28)",
		initial: cpuState{PC: 0x0200, SP: 0xFE},
		ram: map[uint16]uint8{
			0x0200: 0x28, // PLP
			0x0201: 0x77, // dummy byte, read but discarded
			0x01FE: 0x00, // dummy read under current SP, discarded
			0x01FF: 0x81, // pulled status value
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x28, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x77, Write: false}, // T1: dummy read, no PC advance
			{Addr: 0x01FE, Data: 0x00, Write: false}, // T2: dummy read at current SP
			{Addr: 0x01FF, Data: 0x81, Write: false}, // T3: pull status from incremented SP
		},
		final: cpuState{PC: 0x0201, SP: 0xFF, Status: 0x81},
	},
	{
		name:    "AND #$0F ($29)",
		initial: cpuState{PC: 0x0200, A: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x29, // AND #
			0x0201: 0x0F,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x29, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x0F, Write: false}, // T1: operand fetch, AND with A
		},
		final: cpuState{PC: 0x0202, A: 0x0F, Status: 0x00},
	},
	{
		name:    "ROL A ($2A)",
		initial: cpuState{PC: 0x0200, A: 0x81, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x2A, // ROL A
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x2A, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x03, Status: 0x01}, // C flag set from old bit 7
	},
	{
		name:    "BIT $1234 ($2C)",
		initial: cpuState{PC: 0x0200, A: 0x0F},
		ram: map[uint16]uint8{
			0x0200: 0x2C, // BIT abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0xC0, // 11000000: N and V set; A&M == 0 sets Z
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x2C, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0xC0, Write: false}, // T3: read operand, update N/V/Z
		},
		final: cpuState{PC: 0x0203, A: 0x0F, Status: 0xC2}, // N, V, Z flags set
	},
	{
		name:    "AND $1234 ($2D)",
		initial: cpuState{PC: 0x0200, A: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x2D, // AND abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x0F,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x2D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x0F, Write: false}, // T3: read operand, AND with A
		},
		final: cpuState{PC: 0x0203, A: 0x0F, Status: 0x00},
	},
	{
		name:    "ROL $1234 ($2E)",
		initial: cpuState{PC: 0x0200, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x2E, // ROL abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x81, // 10000001, bit 7 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x2E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x81, Write: false}, // T3: read old value
			{Addr: 0x1234, Data: 0x81, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x1234, Data: 0x03, Write: true},  // T5: write rotated value
		},
		final:    cpuState{PC: 0x0203, Status: 0x01}, // C flag set from old bit 7
		finalRAM: map[uint16]uint8{0x1234: 0x03},
	},
	{
		name:    "BMI not taken ($30)",
		initial: cpuState{PC: 0x0200}, // N clear: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0x30, // BMI
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x30, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202},
	},
	{
		name:    "BMI taken, no page cross ($30)",
		initial: cpuState{PC: 0x0200, Status: 0x80}, // N set: branch taken
		ram: map[uint16]uint8{
			0x0200: 0x30, // BMI
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x30, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207, Status: 0x80},
	},
	{
		name:    "BMI taken, page cross ($30)",
		initial: cpuState{PC: 0x02EE, Status: 0x80}, // N set: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0x30, // BMI
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0x30, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310, Status: 0x80},
	},
	{
		name:    "AND ($20),Y no page cross ($31)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x31, // AND (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0x00, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $0300
			0x0301: 0x0F, // operand ANDed into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x31, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0x00, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0301, Data: 0x0F, Write: false}, // T4: read operand, AND with A
		},
		final: cpuState{PC: 0x0202, A: 0x0F, Y: 0x01, Status: 0x00},
	},
	{
		name:    "AND ($20),Y page cross ($31)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x31, // AND (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x0F, // operand ANDed into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x31, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T4: dummy read at wrong address
			{Addr: 0x0400, Data: 0x0F, Write: false}, // T5: read operand at fixed address, AND with A
		},
		final: cpuState{PC: 0x0202, A: 0x0F, Y: 0x01, Status: 0x00},
	},
	{
		name:    "AND $10,X ($35)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x35, // AND zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x0F, // operand ANDed into A, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x35, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x0F, Write: false}, // T3: read operand, AND with A
		},
		final: cpuState{PC: 0x0202, A: 0x0F, X: 0x05, Status: 0x00},
	},
	{
		name:    "ROL $10,X ($36)",
		initial: cpuState{PC: 0x0200, X: 0x05, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x36, // ROL zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x81, // old value at BAL+X, bit 7 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x36, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x81, Write: false}, // T3: read old value
			{Addr: 0x0015, Data: 0x81, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x0015, Data: 0x03, Write: true},  // T5: write rotated value
		},
		final:    cpuState{PC: 0x0202, X: 0x05, Status: 0x01}, // C flag set from old bit 7
		finalRAM: map[uint16]uint8{0x0015: 0x03},
	},
	{
		name:    "SEC ($38)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0x38, // SEC
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x38, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Status: 0x01}, // Carry set
	},
	{
		name:    "AND $02FF,Y no page cross ($39)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x39, // AND abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x0F, // operand ANDed into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x39, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0x0F, Write: false}, // T3: read operand, AND with A
		},
		final: cpuState{PC: 0x0203, A: 0x0F, Y: 0x01, Status: 0x00},
	},
	{
		name:    "AND $03FF,Y page cross ($39)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x39, // AND abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x0F, // operand ANDed into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x39, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x0F, Write: false}, // T4: read operand at fixed address, AND with A
		},
		final: cpuState{PC: 0x0203, A: 0x0F, Y: 0x01, Status: 0x00},
	},
	{
		name:    "AND $02FF,X no page cross ($3D)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x3D, // AND abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x0F, // operand ANDed into A, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x3D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0x0F, Write: false}, // T3: read operand, AND with A
		},
		final: cpuState{PC: 0x0203, A: 0x0F, X: 0x01, Status: 0x00},
	},
	{
		name:    "AND $03FF,X page cross ($3D)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x3D, // AND abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x0F, // operand ANDed into A, at corrected address base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x3D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x0F, Write: false}, // T4: read operand at fixed address, AND with A
		},
		final: cpuState{PC: 0x0203, A: 0x0F, X: 0x01, Status: 0x00},
	},
	{
		name:    "ROL $02FF,X ($3E)",
		initial: cpuState{PC: 0x0200, X: 0x02, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x3E, // ROL abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
			0x0301: 0x81, // old value at base+X, bit 7 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x3E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T3: dummy read at wrong address (reuses byte at $0201)
			{Addr: 0x0301, Data: 0x81, Write: false}, // T4: read old value at fixed address
			{Addr: 0x0301, Data: 0x81, Write: true},  // T5: dummy write-back of old value
			{Addr: 0x0301, Data: 0x03, Write: true},  // T6: write rotated value
		},
		final:    cpuState{PC: 0x0203, X: 0x02, Status: 0x01}, // C flag set from old bit 7
		finalRAM: map[uint16]uint8{0x0301: 0x03},
	},
	{
		name:    "RTI ($40)",
		initial: cpuState{PC: 0x0200, SP: 0xFB},
		ram: map[uint16]uint8{
			0x0200: 0x40, // RTI
			0x0201: 0x77, // dummy byte, read but discarded
			0x01FB: 0x00, // dummy read under current SP, discarded
			0x01FC: 0x80, // pulled status value
			0x01FD: 0x34, // pulled PCL
			0x01FE: 0x12, // pulled PCH
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x40, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x77, Write: false}, // T1: dummy read, no PC advance
			{Addr: 0x01FB, Data: 0x00, Write: false}, // T2: dummy read at current SP
			{Addr: 0x01FC, Data: 0x80, Write: false}, // T3: pull status
			{Addr: 0x01FD, Data: 0x34, Write: false}, // T4: pull PCL
			{Addr: 0x01FE, Data: 0x12, Write: false}, // T5: pull PCH
		},
		final: cpuState{PC: 0x1234, SP: 0xFE, Status: 0x80},
	},
	{
		name:    "EOR ($20,X) ($41)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x04},
		ram: map[uint16]uint8{
			0x0200: 0x41, // EOR (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
			0x0300: 0x0F, // operand EORed into A
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x41, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0x0F, Write: false}, // T5: read operand, EOR with A
		},
		final: cpuState{PC: 0x0202, A: 0xF0, X: 0x04, Status: 0x80}, // N flag set
	},
	{
		name:    "EOR $10 ($45)",
		initial: cpuState{PC: 0x0200, A: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x45, // EOR zp
			0x0201: 0x10,
			0x0010: 0x0F,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x45, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x0F, Write: false}, // T2: read operand, EOR with A
		},
		final: cpuState{PC: 0x0202, A: 0xF0, Status: 0x80}, // N flag set
	},
	{
		name:    "LSR $10 ($46)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0x46, // LSR zp
			0x0201: 0x10,
			0x0010: 0x03, // 00000011, bit 0 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x46, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x03, Write: false}, // T2: read old value
			{Addr: 0x0010, Data: 0x03, Write: true},  // T3: dummy write-back of old value
			{Addr: 0x0010, Data: 0x01, Write: true},  // T4: write shifted value
		},
		final:    cpuState{PC: 0x0202, Status: 0x01}, // C flag set from old bit 0
		finalRAM: map[uint16]uint8{0x0010: 0x01},
	},
	{
		name:    "PHA ($48)",
		initial: cpuState{PC: 0x0200, A: 0x42, SP: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x48, // PHA
			0x0201: 0x77, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x48, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x77, Write: false}, // T1: dummy read, no PC advance
			{Addr: 0x01FF, Data: 0x42, Write: true},  // T2: push A
		},
		final:    cpuState{PC: 0x0201, A: 0x42, SP: 0xFE},
		finalRAM: map[uint16]uint8{0x01FF: 0x42},
	},
	{
		name:    "EOR #$0F ($49)",
		initial: cpuState{PC: 0x0200, A: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x49, // EOR #
			0x0201: 0x0F,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x49, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x0F, Write: false}, // T1: operand fetch, EOR with A
		},
		final: cpuState{PC: 0x0202, A: 0xF0, Status: 0x80}, // N flag set
	},
	{
		name:    "LSR A ($4A)",
		initial: cpuState{PC: 0x0200, A: 0x03}, // 00000011, bit 0 set
		ram: map[uint16]uint8{
			0x0200: 0x4A, // LSR A
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x4A, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x01, Status: 0x01}, // C flag set from old bit 0
	},
	{
		name:    "JMP $1234 ($4C)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0x4C, // JMP abs
			0x0201: 0x34,
			0x0202: 0x12,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x4C, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high, jump
		},
		final: cpuState{PC: 0x1234},
	},
	{
		name:    "EOR $1234 ($4D)",
		initial: cpuState{PC: 0x0200, A: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x4D, // EOR abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x0F,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x4D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x0F, Write: false}, // T3: read operand, EOR with A
		},
		final: cpuState{PC: 0x0203, A: 0xF0, Status: 0x80}, // N flag set
	},
	{
		name:    "LSR $1234 ($4E)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0x4E, // LSR abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x03, // 00000011, bit 0 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x4E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x03, Write: false}, // T3: read old value
			{Addr: 0x1234, Data: 0x03, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x1234, Data: 0x01, Write: true},  // T5: write shifted value
		},
		final:    cpuState{PC: 0x0203, Status: 0x01}, // C flag set from old bit 0
		finalRAM: map[uint16]uint8{0x1234: 0x01},
	},
	{
		name:    "BVC not taken ($50)",
		initial: cpuState{PC: 0x0200, Status: 0x40}, // V set: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0x50, // BVC
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x50, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202, Status: 0x40},
	},
	{
		name:    "BVC taken, no page cross ($50)",
		initial: cpuState{PC: 0x0200}, // V clear: branch taken
		ram: map[uint16]uint8{
			0x0200: 0x50, // BVC
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x50, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207},
	},
	{
		name:    "BVC taken, page cross ($50)",
		initial: cpuState{PC: 0x02EE}, // V clear: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0x50, // BVC
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0x50, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310},
	},
	{
		name:    "EOR ($20),Y no page cross ($51)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x51, // EOR (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0x00, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $0300
			0x0301: 0x0F, // operand EORed into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x51, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0x00, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0301, Data: 0x0F, Write: false}, // T4: read operand, EOR with A
		},
		final: cpuState{PC: 0x0202, A: 0xF0, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "EOR ($20),Y page cross ($51)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x51, // EOR (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x0F, // operand EORed into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x51, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T4: dummy read at wrong address
			{Addr: 0x0400, Data: 0x0F, Write: false}, // T5: read operand at fixed address, EOR with A
		},
		final: cpuState{PC: 0x0202, A: 0xF0, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "EOR $10,X ($55)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x55, // EOR zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x0F, // operand EORed into A, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x55, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x0F, Write: false}, // T3: read operand, EOR with A
		},
		final: cpuState{PC: 0x0202, A: 0xF0, X: 0x05, Status: 0x80}, // N flag set
	},
	{
		name:    "LSR $10,X ($56)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x56, // LSR zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x03, // old value at BAL+X, bit 0 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x56, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x03, Write: false}, // T3: read old value
			{Addr: 0x0015, Data: 0x03, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x0015, Data: 0x01, Write: true},  // T5: write shifted value
		},
		final:    cpuState{PC: 0x0202, X: 0x05, Status: 0x01}, // C flag set from old bit 0
		finalRAM: map[uint16]uint8{0x0015: 0x01},
	},
	{
		name:    "CLI ($58)",
		initial: cpuState{PC: 0x0200, Status: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0x58, // CLI
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x58, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Status: 0xFB}, // Only Interrupt Disable cleared
	},
	{
		name:    "EOR $02FF,Y no page cross ($59)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x59, // EOR abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x0F, // operand EORed into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x59, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0x0F, Write: false}, // T3: read operand, EOR with A
		},
		final: cpuState{PC: 0x0203, A: 0xF0, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "EOR $03FF,Y page cross ($59)",
		initial: cpuState{PC: 0x0200, A: 0xFF, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x59, // EOR abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x0F, // operand EORed into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x59, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x0F, Write: false}, // T4: read operand at fixed address, EOR with A
		},
		final: cpuState{PC: 0x0203, A: 0xF0, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "EOR $02FF,X no page cross ($5D)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x5D, // EOR abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x0F, // operand EORed into A, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x5D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0x0F, Write: false}, // T3: read operand, EOR with A
		},
		final: cpuState{PC: 0x0203, A: 0xF0, X: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "EOR $03FF,X page cross ($5D)",
		initial: cpuState{PC: 0x0200, A: 0xFF, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x5D, // EOR abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x0F, // operand EORed into A, at corrected address base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x5D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x0F, Write: false}, // T4: read operand at fixed address, EOR with A
		},
		final: cpuState{PC: 0x0203, A: 0xF0, X: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "LSR $02FF,X ($5E)",
		initial: cpuState{PC: 0x0200, X: 0x02},
		ram: map[uint16]uint8{
			0x0200: 0x5E, // LSR abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
			0x0301: 0x03, // old value at base+X, bit 0 set
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x5E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T3: dummy read at wrong address (reuses byte at $0201)
			{Addr: 0x0301, Data: 0x03, Write: false}, // T4: read old value at fixed address
			{Addr: 0x0301, Data: 0x03, Write: true},  // T5: dummy write-back of old value
			{Addr: 0x0301, Data: 0x01, Write: true},  // T6: write shifted value
		},
		final:    cpuState{PC: 0x0203, X: 0x02, Status: 0x01}, // C flag set from old bit 0
		finalRAM: map[uint16]uint8{0x0301: 0x01},
	},
	{
		name:    "RTS ($60)",
		initial: cpuState{PC: 0x0200, SP: 0xFB},
		ram: map[uint16]uint8{
			0x0200: 0x60, // RTS
			0x0201: 0x77, // dummy byte, read but discarded
			0x01FB: 0x00, // dummy read under current SP, discarded
			0x01FC: 0x34, // pulled PCL
			0x01FD: 0x12, // pulled PCH => popped PC $1234 (JSR's last byte)
			0x1234: 0x00, // dummy read at popped PC, discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x60, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x77, Write: false}, // T1: dummy read, no PC advance
			{Addr: 0x01FB, Data: 0x00, Write: false}, // T2: dummy read at current SP
			{Addr: 0x01FC, Data: 0x34, Write: false}, // T3: pull PCL
			{Addr: 0x01FD, Data: 0x12, Write: false}, // T4: pull PCH
			{Addr: 0x1234, Data: 0x00, Write: false}, // T5: dummy read at popped PC, then increment
		},
		final: cpuState{PC: 0x1235, SP: 0xFD},
	},
	{
		name:    "ADC ($20,X) ($61)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x04},
		ram: map[uint16]uint8{
			0x0200: 0x61, // ADC (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
			0x0300: 0x05, // operand added into A
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x61, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0x05, Write: false}, // T5: read operand, add with carry
		},
		final: cpuState{PC: 0x0202, A: 0x15, X: 0x04, Status: 0x00},
	},
	{
		name:    "ADC $10 ($65)",
		initial: cpuState{PC: 0x0200, A: 0x10},
		ram: map[uint16]uint8{
			0x0200: 0x65, // ADC zp
			0x0201: 0x10,
			0x0010: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x65, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x05, Write: false}, // T2: read operand, add with carry
		},
		final: cpuState{PC: 0x0202, A: 0x15, Status: 0x00},
	},
	{
		name:    "ROR $10 ($66)",
		initial: cpuState{PC: 0x0200, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x66, // ROR zp
			0x0201: 0x10,
			0x0010: 0x02, // 00000010, bit 0 clear
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x66, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x02, Write: false}, // T2: read old value
			{Addr: 0x0010, Data: 0x02, Write: true},  // T3: dummy write-back of old value
			{Addr: 0x0010, Data: 0x81, Write: true},  // T4: write rotated value
		},
		final:    cpuState{PC: 0x0202, Status: 0x80}, // N flag set (carry in rotated into bit 7), C clear
		finalRAM: map[uint16]uint8{0x0010: 0x81},
	},
	{
		name:    "PLA ($68)",
		initial: cpuState{PC: 0x0200, SP: 0xFE},
		ram: map[uint16]uint8{
			0x0200: 0x68, // PLA
			0x0201: 0x77, // dummy byte, read but discarded
			0x01FE: 0x00, // dummy read under current SP, discarded
			0x01FF: 0x80, // pulled A value, negative
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x68, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x77, Write: false}, // T1: dummy read, no PC advance
			{Addr: 0x01FE, Data: 0x00, Write: false}, // T2: dummy read at current SP
			{Addr: 0x01FF, Data: 0x80, Write: false}, // T3: pull A from incremented SP
		},
		final: cpuState{PC: 0x0201, SP: 0xFF, A: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "ADC #$05 ($69)",
		initial: cpuState{PC: 0x0200, A: 0x10},
		ram: map[uint16]uint8{
			0x0200: 0x69, // ADC #
			0x0201: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x69, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: operand fetch, add with carry
		},
		final: cpuState{PC: 0x0202, A: 0x15, Status: 0x00},
	},
	{
		name:    "ROR A ($6A)",
		initial: cpuState{PC: 0x0200, A: 0x02, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x6A, // ROR A
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x6A, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x81, Status: 0x80}, // N flag set, C clear
	},
	{
		name:    "JMP ($02FF) ($6C)",
		initial: cpuState{PC: 0x1000},
		ram: map[uint16]uint8{
			0x1000: 0x6C, // JMP indirect
			0x1001: 0xFF, // pointer address low byte
			0x1002: 0x02, // pointer address high byte => pointer $02FF
			0x02FF: 0x34, // target address low byte
			0x0200: 0x12, // target address high byte (page-wrap bug: pointer+1 wraps within page)
		},
		cycles: []busCycle{
			{Addr: 0x1000, Data: 0x6C, Write: false}, // T0: opcode fetch
			{Addr: 0x1001, Data: 0xFF, Write: false}, // T1: fetch pointer low
			{Addr: 0x1002, Data: 0x02, Write: false}, // T2: fetch pointer high
			{Addr: 0x02FF, Data: 0x34, Write: false}, // T3: fetch target address low
			{Addr: 0x0200, Data: 0x12, Write: false}, // T4: fetch target address high (wraps within page)
		},
		final: cpuState{PC: 0x1234},
	},
	{
		name:    "ADC $1234 ($6D)",
		initial: cpuState{PC: 0x0200, A: 0x10},
		ram: map[uint16]uint8{
			0x0200: 0x6D, // ADC abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x6D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x05, Write: false}, // T3: read operand, add with carry
		},
		final: cpuState{PC: 0x0203, A: 0x15, Status: 0x00},
	},
	{
		name:    "ROR $1234 ($6E)",
		initial: cpuState{PC: 0x0200, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x6E, // ROR abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x02, // 00000010, bit 0 clear
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x6E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x02, Write: false}, // T3: read old value
			{Addr: 0x1234, Data: 0x02, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x1234, Data: 0x81, Write: true},  // T5: write rotated value
		},
		final:    cpuState{PC: 0x0203, Status: 0x80}, // N flag set, C clear
		finalRAM: map[uint16]uint8{0x1234: 0x81},
	},
	{
		name:    "BVS not taken ($70)",
		initial: cpuState{PC: 0x0200}, // V clear: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0x70, // BVS
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x70, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202},
	},
	{
		name:    "BVS taken, no page cross ($70)",
		initial: cpuState{PC: 0x0200, Status: 0x40}, // V set: branch taken
		ram: map[uint16]uint8{
			0x0200: 0x70, // BVS
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x70, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207, Status: 0x40},
	},
	{
		name:    "BVS taken, page cross ($70)",
		initial: cpuState{PC: 0x02EE, Status: 0x40}, // V set: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0x70, // BVS
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0x70, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310, Status: 0x40},
	},
	{
		name:    "ADC ($20),Y no page cross ($71)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x71, // ADC (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0x00, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $0300
			0x0301: 0x05, // operand added into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x71, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0x00, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0301, Data: 0x05, Write: false}, // T4: read operand, add with carry
		},
		final: cpuState{PC: 0x0202, A: 0x15, Y: 0x01, Status: 0x00},
	},
	{
		name:    "ADC ($20),Y page cross ($71)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x71, // ADC (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand added into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x71, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T4: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T5: read operand at fixed address, add with carry
		},
		final: cpuState{PC: 0x0202, A: 0x15, Y: 0x01, Status: 0x00},
	},
	{
		name:    "ADC $10,X ($75)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x75, // ADC zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x05, // operand added into A, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x75, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x05, Write: false}, // T3: read operand, add with carry
		},
		final: cpuState{PC: 0x0202, A: 0x15, X: 0x05, Status: 0x00},
	},
	{
		name:    "ROR $10,X ($76)",
		initial: cpuState{PC: 0x0200, X: 0x05, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x76, // ROR zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x02, // old value at BAL+X, bit 0 clear
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x76, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x02, Write: false}, // T3: read old value
			{Addr: 0x0015, Data: 0x02, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x0015, Data: 0x81, Write: true},  // T5: write rotated value
		},
		final:    cpuState{PC: 0x0202, X: 0x05, Status: 0x80}, // N flag set, C clear
		finalRAM: map[uint16]uint8{0x0015: 0x81},
	},
	{
		name:    "SEI ($78)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0x78, // SEI
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x78, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Status: 0x04}, // Interrupt Disable set
	},
	{
		name:    "ADC $02FF,Y no page cross ($79)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x79, // ADC abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x05, // operand added into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x79, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0x05, Write: false}, // T3: read operand, add with carry
		},
		final: cpuState{PC: 0x0203, A: 0x15, Y: 0x01, Status: 0x00},
	},
	{
		name:    "ADC $03FF,Y page cross ($79)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x79, // ADC abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand added into A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x79, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T4: read operand at fixed address, add with carry
		},
		final: cpuState{PC: 0x0203, A: 0x15, Y: 0x01, Status: 0x00},
	},
	{
		name:    "ADC $02FF,X no page cross ($7D)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x7D, // ADC abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x05, // operand added into A, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x7D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0x05, Write: false}, // T3: read operand, add with carry
		},
		final: cpuState{PC: 0x0203, A: 0x15, X: 0x01, Status: 0x00},
	},
	{
		name:    "ADC $03FF,X page cross ($7D)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x7D, // ADC abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand added into A, at corrected address base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x7D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T4: read operand at fixed address, add with carry
		},
		final: cpuState{PC: 0x0203, A: 0x15, X: 0x01, Status: 0x00},
	},
	{
		name:    "ROR $02FF,X ($7E)",
		initial: cpuState{PC: 0x0200, X: 0x02, Status: 0x01}, // Carry in set
		ram: map[uint16]uint8{
			0x0200: 0x7E, // ROR abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
			0x0301: 0x02, // old value at base+X, bit 0 clear
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x7E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T3: dummy read at wrong address (reuses byte at $0201)
			{Addr: 0x0301, Data: 0x02, Write: false}, // T4: read old value at fixed address
			{Addr: 0x0301, Data: 0x02, Write: true},  // T5: dummy write-back of old value
			{Addr: 0x0301, Data: 0x81, Write: true},  // T6: write rotated value
		},
		final:    cpuState{PC: 0x0203, X: 0x02, Status: 0x80}, // N flag set, C clear
		finalRAM: map[uint16]uint8{0x0301: 0x81},
	},
	{
		name:    "STA ($20,X) ($81)",
		initial: cpuState{PC: 0x0200, A: 0x42, X: 0x04},
		ram: map[uint16]uint8{
			0x0200: 0x81, // STA (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x81, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0x42, Write: true},  // T5: write A
		},
		final:    cpuState{PC: 0x0202, A: 0x42, X: 0x04},
		finalRAM: map[uint16]uint8{0x0300: 0x42},
	},
	{
		name:    "STY $10 ($84)",
		initial: cpuState{PC: 0x0200, Y: 0x42},
		ram: map[uint16]uint8{
			0x0200: 0x84, // STY zp
			0x0201: 0x10,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x84, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x42, Write: true},  // T2: write Y
		},
		final:    cpuState{PC: 0x0202, Y: 0x42},
		finalRAM: map[uint16]uint8{0x0010: 0x42},
	},
	{
		name:    "STA $10 ($85)",
		initial: cpuState{PC: 0x0200, A: 0x42},
		ram: map[uint16]uint8{
			0x0200: 0x85, // STA zp
			0x0201: 0x10,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x85, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x42, Write: true},  // T2: write A
		},
		final:    cpuState{PC: 0x0202, A: 0x42},
		finalRAM: map[uint16]uint8{0x0010: 0x42},
	},
	{
		name:    "STX $10 ($86)",
		initial: cpuState{PC: 0x0200, X: 0x42},
		ram: map[uint16]uint8{
			0x0200: 0x86, // STX zp
			0x0201: 0x10,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x86, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x42, Write: true},  // T2: write X
		},
		final:    cpuState{PC: 0x0202, X: 0x42},
		finalRAM: map[uint16]uint8{0x0010: 0x42},
	},
	{
		name:    "DEY ($88)",
		initial: cpuState{PC: 0x0200, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x88, // DEY
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x88, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Y: 0x00, Status: 0x02}, // Z flag set
	},
	{
		name:    "TXA ($8A)",
		initial: cpuState{PC: 0x0200, X: 0x80},
		ram: map[uint16]uint8{
			0x0200: 0x8A, // TXA
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x8A, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x80, X: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "STY $1234 ($8C)",
		initial: cpuState{PC: 0x0200, Y: 0x42},
		ram: map[uint16]uint8{
			0x0200: 0x8C, // STY abs
			0x0201: 0x34,
			0x0202: 0x12,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x8C, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x42, Write: true},  // T3: write Y
		},
		final:    cpuState{PC: 0x0203, Y: 0x42},
		finalRAM: map[uint16]uint8{0x1234: 0x42},
	},
	{
		name:    "STA $1234 ($8D)",
		initial: cpuState{PC: 0x0200, A: 0x42},
		ram: map[uint16]uint8{
			0x0200: 0x8D, // STA abs
			0x0201: 0x34,
			0x0202: 0x12,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x8D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x42, Write: true},  // T3: write A
		},
		final:    cpuState{PC: 0x0203, A: 0x42},
		finalRAM: map[uint16]uint8{0x1234: 0x42},
	},
	{
		name:    "STX $1234 ($8E)",
		initial: cpuState{PC: 0x0200, X: 0x42},
		ram: map[uint16]uint8{
			0x0200: 0x8E, // STX abs
			0x0201: 0x34,
			0x0202: 0x12,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x8E, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x42, Write: true},  // T3: write X
		},
		final:    cpuState{PC: 0x0203, X: 0x42},
		finalRAM: map[uint16]uint8{0x1234: 0x42},
	},
	{
		name:    "BCC not taken ($90)",
		initial: cpuState{PC: 0x0200, Status: 0x01}, // C set: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0x90, // BCC
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x90, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202, Status: 0x01},
	},
	{
		name:    "BCC taken, no page cross ($90)",
		initial: cpuState{PC: 0x0200}, // C clear: branch taken
		ram: map[uint16]uint8{
			0x0200: 0x90, // BCC
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x90, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207},
	},
	{
		name:    "BCC taken, page cross ($90)",
		initial: cpuState{PC: 0x02EE}, // C clear: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0x90, // BCC
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0x90, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310},
	},
	{
		name:    "STA ($20),Y page cross ($91)",
		initial: cpuState{PC: 0x0200, A: 0x42, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x91, // STA (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x91, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T4: dummy read at wrong address (store always pays this cycle)
			{Addr: 0x0400, Data: 0x42, Write: true},  // T5: write A at corrected address
		},
		final:    cpuState{PC: 0x0202, A: 0x42, Y: 0x01},
		finalRAM: map[uint16]uint8{0x0400: 0x42},
	},
	{
		name:    "STY $10,X ($94)",
		initial: cpuState{PC: 0x0200, Y: 0x42, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x94, // STY zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x94, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x42, Write: true},  // T3: write Y, at BAL+X
		},
		final:    cpuState{PC: 0x0202, Y: 0x42, X: 0x05},
		finalRAM: map[uint16]uint8{0x0015: 0x42},
	},
	{
		name:    "STA $10,X ($95)",
		initial: cpuState{PC: 0x0200, A: 0x42, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x95, // STA zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x95, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x42, Write: true},  // T3: write A, at BAL+X
		},
		final:    cpuState{PC: 0x0202, A: 0x42, X: 0x05},
		finalRAM: map[uint16]uint8{0x0015: 0x42},
	},
	{
		name:    "STX $10,Y ($96)",
		initial: cpuState{PC: 0x0200, X: 0x42, Y: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0x96, // STX zp,Y
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x96, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x42, Write: true},  // T3: write X, at BAL+Y
		},
		final:    cpuState{PC: 0x0202, X: 0x42, Y: 0x05},
		finalRAM: map[uint16]uint8{0x0015: 0x42},
	},
	{
		name:    "TYA ($98)",
		initial: cpuState{PC: 0x0200, Y: 0x80},
		ram: map[uint16]uint8{
			0x0200: 0x98, // TYA
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x98, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x80, Y: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "STA $02FF,Y page cross ($99)",
		initial: cpuState{PC: 0x0200, A: 0x42, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x99, // STA abs,Y (also serves as the dummy-read byte at the wrapped guess address)
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x99, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0200, Data: 0x99, Write: false}, // T3: dummy read at wrong address (wraps back to $0200)
			{Addr: 0x0300, Data: 0x42, Write: true},  // T4: write A at corrected address
		},
		final:    cpuState{PC: 0x0203, A: 0x42, Y: 0x01},
		finalRAM: map[uint16]uint8{0x0300: 0x42},
	},
	{
		name:    "TXS ($9A)",
		initial: cpuState{PC: 0x0200, X: 0x80},
		ram: map[uint16]uint8{
			0x0200: 0x9A, // TXS
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x9A, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, X: 0x80, SP: 0x80}, // No flags affected
	},
	{
		name:    "STA $02FF,X page cross ($9D)",
		initial: cpuState{PC: 0x0200, A: 0x42, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0x9D, // STA abs,X (also serves as the dummy-read byte at the wrapped guess address)
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0x9D, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0200, Data: 0x9D, Write: false}, // T3: dummy read at wrong address (wraps back to $0200)
			{Addr: 0x0300, Data: 0x42, Write: true},  // T4: write A at corrected address
		},
		final:    cpuState{PC: 0x0203, A: 0x42, X: 0x01},
		finalRAM: map[uint16]uint8{0x0300: 0x42},
	},
	{
		name:    "LDY #$80 ($A0)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA0, // LDY #
			0x0201: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x80, Write: false}, // T1: operand fetch
		},
		final: cpuState{PC: 0x0202, Y: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDA ($20,X) ($A1)",
		initial: cpuState{PC: 0x0200, X: 0x04},
		ram: map[uint16]uint8{
			0x0200: 0xA1, // LDA (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
			0x0300: 0x80, // operand loaded into A
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0x80, Write: false}, // T5: read operand into A
		},
		final: cpuState{PC: 0x0202, A: 0x80, X: 0x04, Status: 0x80}, // N flag set
	},
	{
		name:    "LDX #$80 ($A2)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA2, // LDX #
			0x0201: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA2, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x80, Write: false}, // T1: operand fetch
		},
		final: cpuState{PC: 0x0202, X: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDY $10 ($A4)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA4, // LDY zp
			0x0201: 0x10,
			0x0010: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA4, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x80, Write: false}, // T2: read operand into Y
		},
		final: cpuState{PC: 0x0202, Y: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDX $10 ($A6)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA6, // LDX zp
			0x0201: 0x10,
			0x0010: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA6, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x80, Write: false}, // T2: read operand into X
		},
		final: cpuState{PC: 0x0202, X: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "TAY ($A8)",
		initial: cpuState{PC: 0x0200, A: 0x80},
		ram: map[uint16]uint8{
			0x0200: 0xA8, // TAY
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA8, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x80, Y: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "TAX ($AA)",
		initial: cpuState{PC: 0x0200, A: 0x80},
		ram: map[uint16]uint8{
			0x0200: 0xAA, // TAX
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xAA, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, A: 0x80, X: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDY $1234 ($AC)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xAC, // LDY abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xAC, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x80, Write: false}, // T3: read operand into Y
		},
		final: cpuState{PC: 0x0203, Y: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDA $1234 ($AD)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xAD, // LDA abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xAD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x80, Write: false}, // T3: read operand into A
		},
		final: cpuState{PC: 0x0203, A: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDX $1234 ($AE)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xAE, // LDX abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xAE, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x80, Write: false}, // T3: read operand into X
		},
		final: cpuState{PC: 0x0203, X: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "BCS not taken ($B0)",
		initial: cpuState{PC: 0x0200}, // C clear: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0xB0, // BCS
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202},
	},
	{
		name:    "BCS taken, no page cross ($B0)",
		initial: cpuState{PC: 0x0200, Status: 0x01}, // C set: branch taken
		ram: map[uint16]uint8{
			0x0200: 0xB0, // BCS
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207, Status: 0x01},
	},
	{
		name:    "BCS taken, page cross ($B0)",
		initial: cpuState{PC: 0x02EE, Status: 0x01}, // C set: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0xB0, // BCS
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0xB0, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310, Status: 0x01},
	},
	{
		name:    "LDA ($20),Y no page cross ($B1)",
		initial: cpuState{PC: 0x0200, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xB1, // LDA (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0x00, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $0300
			0x0301: 0x80, // operand loaded into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0x00, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0301, Data: 0x80, Write: false}, // T4: read operand into A
		},
		final: cpuState{PC: 0x0202, A: 0x80, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "LDA ($20),Y page cross ($B1)",
		initial: cpuState{PC: 0x0200, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xB1, // LDA (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x30, // effective address high byte (BAH) => base $30FF
			0x3000: 0x00, // dummy read at guess address (wrong page), discarded
			0x3100: 0x77, // operand loaded into A, at corrected base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x30, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x3000, Data: 0x00, Write: false}, // T4: dummy read at guess address (page crossed)
			{Addr: 0x3100, Data: 0x77, Write: false}, // T5: re-read operand at corrected address
		},
		final: cpuState{PC: 0x0202, A: 0x77, Y: 0x01},
	},
	{
		name:    "LDY $10,X ($B4)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xB4, // LDY zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x80, // operand loaded into Y, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB4, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x80, Write: false}, // T3: read operand into Y
		},
		final: cpuState{PC: 0x0202, Y: 0x80, X: 0x05, Status: 0x80}, // N flag set
	},
	{
		name:    "LDA $10,X ($B5)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xB5, // LDA zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x80, // operand loaded into A, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB5, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x80, Write: false}, // T3: read operand into A
		},
		final: cpuState{PC: 0x0202, A: 0x80, X: 0x05, Status: 0x80}, // N flag set
	},
	{
		name:    "LDX $10,Y ($B6)",
		initial: cpuState{PC: 0x0200, Y: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xB6, // LDX zp,Y
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x80, // operand loaded into X, at BAL+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB6, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x80, Write: false}, // T3: read operand into X
		},
		final: cpuState{PC: 0x0202, X: 0x80, Y: 0x05, Status: 0x80}, // N flag set
	},
	{
		name:    "CLV ($B8)",
		initial: cpuState{PC: 0x0200, Status: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0xB8, // CLV
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB8, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Status: 0xBF}, // Only Overflow cleared
	},
	{
		name:    "LDA $02FF,Y no page cross ($B9)",
		initial: cpuState{PC: 0x0200, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xB9, // LDA abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x80, // operand loaded into A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0x80, Write: false}, // T3: read operand into A
		},
		final: cpuState{PC: 0x0203, A: 0x80, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "LDA $30FF,Y page cross ($B9)",
		initial: cpuState{PC: 0x0200, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xB9, // LDA abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x30, // address high byte => base $30FF
			0x3000: 0x00, // dummy read at guess address (wrong page), discarded
			0x3100: 0x77, // operand loaded into A, at corrected base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xB9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x30, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x3000, Data: 0x00, Write: false}, // T3: dummy read at guess address (page crossed)
			{Addr: 0x3100, Data: 0x77, Write: false}, // T4: re-read operand at corrected address
		},
		final: cpuState{PC: 0x0203, A: 0x77, Y: 0x01},
	},
	{
		name:    "TSX ($BA)",
		initial: cpuState{PC: 0x0200, SP: 0x80},
		ram: map[uint16]uint8{
			0x0200: 0xBA, // TSX
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xBA, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, X: 0x80, SP: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDY $02FF,X no page cross ($BC)",
		initial: cpuState{PC: 0x0200, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xBC, // LDY abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x80, // operand loaded into Y, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xBC, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0x80, Write: false}, // T3: read operand into Y
		},
		final: cpuState{PC: 0x0203, Y: 0x80, X: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "LDY $30FF,X page cross ($BC)",
		initial: cpuState{PC: 0x0200, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xBC, // LDY abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x30, // address high byte => base $30FF
			0x3000: 0x00, // dummy read at guess address (wrong page), discarded
			0x3100: 0x77, // operand loaded into Y, at corrected base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xBC, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x30, Write: false}, // T2: fetch address high, add X
			{Addr: 0x3000, Data: 0x00, Write: false}, // T3: dummy read at guess address (page crossed)
			{Addr: 0x3100, Data: 0x77, Write: false}, // T4: re-read operand at corrected address
		},
		final: cpuState{PC: 0x0203, Y: 0x77, X: 0x01},
	},
	{
		name:    "LDA $02FF,X no page cross ($BD)",
		initial: cpuState{PC: 0x0200, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xBD, // LDA abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x80, // operand loaded into A, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xBD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0x80, Write: false}, // T3: read operand into A
		},
		final: cpuState{PC: 0x0203, A: 0x80, X: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "LDA $30FF,X page cross ($BD)",
		initial: cpuState{PC: 0x0200, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xBD, // LDA abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x30, // address high byte => base $30FF
			0x3000: 0x00, // dummy read at guess address (wrong page), discarded
			0x3100: 0x77, // operand loaded into A, at corrected base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xBD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x30, Write: false}, // T2: fetch address high, add X
			{Addr: 0x3000, Data: 0x00, Write: false}, // T3: dummy read at guess address (page crossed)
			{Addr: 0x3100, Data: 0x77, Write: false}, // T4: re-read operand at corrected address
		},
		final: cpuState{PC: 0x0203, A: 0x77, X: 0x01},
	},
	{
		name:    "LDX $02FF,Y no page cross ($BE)",
		initial: cpuState{PC: 0x0200, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xBE, // LDX abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x80, // operand loaded into X, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xBE, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0x80, Write: false}, // T3: read operand into X
		},
		final: cpuState{PC: 0x0203, X: 0x80, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "LDX $03FF,Y page cross ($BE)",
		initial: cpuState{PC: 0x0200, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xBE, // LDX abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x80, // operand loaded into X, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xBE, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x80, Write: false}, // T4: read operand at fixed address into X
		},
		final: cpuState{PC: 0x0203, X: 0x80, Y: 0x01, Status: 0x80}, // N flag set
	},
	{
		name:    "CPY #$05 ($C0)",
		initial: cpuState{PC: 0x0200, Y: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xC0, // CPY #
			0x0201: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xC0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: operand fetch, compare with Y
		},
		final: cpuState{PC: 0x0202, Y: 0x05, Status: 0x03}, // C and Z set (Y == M)
	},
	{
		name:    "CMP ($20,X) ($C1)",
		initial: cpuState{PC: 0x0200, A: 0x05, X: 0x04},
		ram: map[uint16]uint8{
			0x0200: 0xC1, // CMP (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
			0x0300: 0x05, // operand compared with A
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xC1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0x05, Write: false}, // T5: read operand, compare with A
		},
		final: cpuState{PC: 0x0202, A: 0x05, X: 0x04, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "CPY $10 ($C4)",
		initial: cpuState{PC: 0x0200, Y: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xC4, // CPY zp
			0x0201: 0x10,
			0x0010: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xC4, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x05, Write: false}, // T2: read operand, compare with Y
		},
		final: cpuState{PC: 0x0202, Y: 0x05, Status: 0x03}, // C and Z set (Y == M)
	},
	{
		name:    "CMP $10 ($C5)",
		initial: cpuState{PC: 0x0200, A: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xC5, // CMP zp
			0x0201: 0x10,
			0x0010: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xC5, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x05, Write: false}, // T2: read operand, compare with A
		},
		final: cpuState{PC: 0x0202, A: 0x05, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "DEC $10 ($C6)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xC6, // DEC zp
			0x0201: 0x10,
			0x0010: 0x01,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xC6, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x01, Write: false}, // T2: read old value
			{Addr: 0x0010, Data: 0x01, Write: true},  // T3: dummy write-back of old value
			{Addr: 0x0010, Data: 0x00, Write: true},  // T4: write decremented value
		},
		final:    cpuState{PC: 0x0202, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x0010: 0x00},
	},
	{
		name:    "INY ($C8)",
		initial: cpuState{PC: 0x0200, Y: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0xC8, // INY
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xC8, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Y: 0x00, Status: 0x02}, // Z flag set
	},
	{
		name:    "CMP #$05 ($C9)",
		initial: cpuState{PC: 0x0200, A: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xC9, // CMP #
			0x0201: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xC9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: operand fetch, compare with A
		},
		final: cpuState{PC: 0x0202, A: 0x05, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "DEX ($CA)",
		initial: cpuState{PC: 0x0200, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xCA, // DEX
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xCA, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, X: 0x00, Status: 0x02}, // Z flag set
	},
	{
		name:    "CPY $1234 ($CC)",
		initial: cpuState{PC: 0x0200, Y: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xCC, // CPY abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xCC, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x05, Write: false}, // T3: read operand, compare with Y
		},
		final: cpuState{PC: 0x0203, Y: 0x05, Status: 0x03}, // C and Z set (Y == M)
	},
	{
		name:    "CMP $1234 ($CD)",
		initial: cpuState{PC: 0x0200, A: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xCD, // CMP abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xCD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x05, Write: false}, // T3: read operand, compare with A
		},
		final: cpuState{PC: 0x0203, A: 0x05, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "DEC $1234 ($CE)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xCE, // DEC abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x01,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xCE, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x01, Write: false}, // T3: read old value
			{Addr: 0x1234, Data: 0x01, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x1234, Data: 0x00, Write: true},  // T5: write decremented value
		},
		final:    cpuState{PC: 0x0203, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x1234: 0x00},
	},
	{
		name:    "BNE not taken ($D0)",
		initial: cpuState{PC: 0x0200, Status: 0x02}, // Z set: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0xD0, // BNE
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202, Status: 0x02},
	},
	{
		name:    "BNE taken, no page cross ($D0)",
		initial: cpuState{PC: 0x0200}, // Z clear: branch taken
		ram: map[uint16]uint8{
			0x0200: 0xD0, // BNE
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207},
	},
	{
		name:    "BNE taken, page cross ($D0)",
		initial: cpuState{PC: 0x02EE}, // Z clear: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0xD0, // BNE
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0xD0, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310},
	},
	{
		name:    "CMP ($20),Y no page cross ($D1)",
		initial: cpuState{PC: 0x0200, A: 0x05, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xD1, // CMP (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0x00, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $0300
			0x0301: 0x05, // operand compared with A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0x00, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0301, Data: 0x05, Write: false}, // T4: read operand, compare with A
		},
		final: cpuState{PC: 0x0202, A: 0x05, Y: 0x01, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "CMP ($20),Y page cross ($D1)",
		initial: cpuState{PC: 0x0200, A: 0x05, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xD1, // CMP (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand compared with A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T4: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T5: read operand at fixed address, compare with A
		},
		final: cpuState{PC: 0x0202, A: 0x05, Y: 0x01, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "CMP $10,X ($D5)",
		initial: cpuState{PC: 0x0200, A: 0x05, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xD5, // CMP zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x05, // operand compared with A, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD5, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x05, Write: false}, // T3: read operand, compare with A
		},
		final: cpuState{PC: 0x0202, A: 0x05, X: 0x05, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "DEC $10,X ($D6)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xD6, // DEC zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x01, // old value at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD6, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x01, Write: false}, // T3: read old value
			{Addr: 0x0015, Data: 0x01, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x0015, Data: 0x00, Write: true},  // T5: write decremented value
		},
		final:    cpuState{PC: 0x0202, X: 0x05, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x0015: 0x00},
	},
	{
		name:    "CLD ($D8)",
		initial: cpuState{PC: 0x0200, Status: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0xD8, // CLD
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD8, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Status: 0xF7}, // Only Decimal mode cleared
	},
	{
		name:    "CMP $02FF,Y no page cross ($D9)",
		initial: cpuState{PC: 0x0200, A: 0x05, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xD9, // CMP abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x05, // operand compared with A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0x05, Write: false}, // T3: read operand, compare with A
		},
		final: cpuState{PC: 0x0203, A: 0x05, Y: 0x01, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "CMP $03FF,Y page cross ($D9)",
		initial: cpuState{PC: 0x0200, A: 0x05, Y: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xD9, // CMP abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand compared with A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xD9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T4: read operand at fixed address, compare with A
		},
		final: cpuState{PC: 0x0203, A: 0x05, Y: 0x01, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "CMP $02FF,X no page cross ($DD)",
		initial: cpuState{PC: 0x0200, A: 0x05, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xDD, // CMP abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x05, // operand compared with A, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xDD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0x05, Write: false}, // T3: read operand, compare with A
		},
		final: cpuState{PC: 0x0203, A: 0x05, X: 0x01, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "CMP $03FF,X page cross ($DD)",
		initial: cpuState{PC: 0x0200, A: 0x05, X: 0x01},
		ram: map[uint16]uint8{
			0x0200: 0xDD, // CMP abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand compared with A, at corrected address base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xDD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T4: read operand at fixed address, compare with A
		},
		final: cpuState{PC: 0x0203, A: 0x05, X: 0x01, Status: 0x03}, // C and Z set (A == M)
	},
	{
		name:    "DEC $02FF,X ($DE)",
		initial: cpuState{PC: 0x0200, X: 0x02},
		ram: map[uint16]uint8{
			0x0200: 0xDE, // DEC abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
			0x0301: 0x01, // old value at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xDE, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T3: dummy read at wrong address (reuses byte at $0201)
			{Addr: 0x0301, Data: 0x01, Write: false}, // T4: read old value at fixed address
			{Addr: 0x0301, Data: 0x01, Write: true},  // T5: dummy write-back of old value
			{Addr: 0x0301, Data: 0x00, Write: true},  // T6: write decremented value
		},
		final:    cpuState{PC: 0x0203, X: 0x02, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x0301: 0x00},
	},
	{
		name:    "CPX #$05 ($E0)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xE0, // CPX #
			0x0201: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xE0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: operand fetch, compare with X
		},
		final: cpuState{PC: 0x0202, X: 0x05, Status: 0x03}, // C and Z set (X == M)
	},
	{
		name:    "SBC ($20,X) ($E1)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x04, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xE1, // SBC (zp,X)
			0x0201: 0x20, // pointer base address (BAL)
			0x0020: 0x99, // dummy read while X is added, discarded
			0x0024: 0x00, // effective address low byte, at BAL+X
			0x0025: 0x03, // effective address high byte, at BAL+X+1
			0x0300: 0x05, // operand subtracted from A
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xE1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (BAL)
			{Addr: 0x0020, Data: 0x99, Write: false}, // T2: dummy read from BAL, discarded
			{Addr: 0x0024, Data: 0x00, Write: false}, // T3: fetch effective address low
			{Addr: 0x0025, Data: 0x03, Write: false}, // T4: fetch effective address high
			{Addr: 0x0300, Data: 0x05, Write: false}, // T5: read operand, subtract with borrow
		},
		final: cpuState{PC: 0x0202, A: 0x0B, X: 0x04, Status: 0x01},
	},
	{
		name:    "CPX $10 ($E4)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xE4, // CPX zp
			0x0201: 0x10,
			0x0010: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xE4, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x05, Write: false}, // T2: read operand, compare with X
		},
		final: cpuState{PC: 0x0202, X: 0x05, Status: 0x03}, // C and Z set (X == M)
	},
	{
		name:    "SBC $10 ($E5)",
		initial: cpuState{PC: 0x0200, A: 0x10, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xE5, // SBC zp
			0x0201: 0x10,
			0x0010: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xE5, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x05, Write: false}, // T2: read operand, subtract with borrow
		},
		final: cpuState{PC: 0x0202, A: 0x0B, Status: 0x01},
	},
	{
		name:    "INC $10 ($E6)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xE6, // INC zp
			0x0201: 0x10,
			0x0010: 0xFF,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xE6, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0xFF, Write: false}, // T2: read old value
			{Addr: 0x0010, Data: 0xFF, Write: true},  // T3: dummy write-back of old value
			{Addr: 0x0010, Data: 0x00, Write: true},  // T4: write incremented value
		},
		final:    cpuState{PC: 0x0202, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x0010: 0x00},
	},
	{
		name:    "INX ($E8)",
		initial: cpuState{PC: 0x0200, X: 0xFF},
		ram: map[uint16]uint8{
			0x0200: 0xE8, // INX
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xE8, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, X: 0x00, Status: 0x02}, // Z flag set
	},
	{
		name:    "SBC #$05 ($E9)",
		initial: cpuState{PC: 0x0200, A: 0x10, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xE9, // SBC #
			0x0201: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xE9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: operand fetch, subtract with borrow
		},
		final: cpuState{PC: 0x0202, A: 0x0B, Status: 0x01},
	},
	{
		name:    "CPX $1234 ($EC)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xEC, // CPX abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xEC, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x05, Write: false}, // T3: read operand, compare with X
		},
		final: cpuState{PC: 0x0203, X: 0x05, Status: 0x03}, // C and Z set (X == M)
	},
	{
		name:    "SBC $1234 ($ED)",
		initial: cpuState{PC: 0x0200, A: 0x10, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xED, // SBC abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0x05,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xED, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0x05, Write: false}, // T3: read operand, subtract with borrow
		},
		final: cpuState{PC: 0x0203, A: 0x0B, Status: 0x01},
	},
	{
		name:    "INC $1234 ($EE)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xEE, // INC abs
			0x0201: 0x34,
			0x0202: 0x12,
			0x1234: 0xFF,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xEE, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x34, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x12, Write: false}, // T2: fetch address high
			{Addr: 0x1234, Data: 0xFF, Write: false}, // T3: read old value
			{Addr: 0x1234, Data: 0xFF, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x1234, Data: 0x00, Write: true},  // T5: write incremented value
		},
		final:    cpuState{PC: 0x0203, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x1234: 0x00},
	},
	{
		name:    "BEQ not taken ($F0)",
		initial: cpuState{PC: 0x0200}, // Z clear: branch not taken
		ram: map[uint16]uint8{
			0x0200: 0xF0, // BEQ
			0x0201: 0x05, // offset, irrelevant since not taken
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
		},
		final: cpuState{PC: 0x0202},
	},
	{
		name:    "BEQ taken, no page cross ($F0)",
		initial: cpuState{PC: 0x0200, Status: 0x02}, // Z set: branch taken
		ram: map[uint16]uint8{
			0x0200: 0xF0, // BEQ
			0x0201: 0x05, // offset +5
			0x0202: 0x00, // dummy byte at "next instruction" address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF0, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x05, Write: false}, // T1: fetch offset
			{Addr: 0x0202, Data: 0x00, Write: false}, // T2: dummy read, compute new PC
		},
		final: cpuState{PC: 0x0207, Status: 0x02},
	},
	{
		name:    "BEQ taken, page cross ($F0)",
		initial: cpuState{PC: 0x02EE, Status: 0x02}, // Z set: branch taken
		ram: map[uint16]uint8{
			0x02EE: 0xF0, // BEQ
			0x02EF: 0x20, // offset +0x20, crosses page from $02F0 to $0310
			0x02F0: 0x00, // dummy byte at "next instruction" address
			0x0210: 0x00, // dummy byte at "wrong" (unfixed high byte) address
		},
		cycles: []busCycle{
			{Addr: 0x02EE, Data: 0xF0, Write: false}, // T0: opcode fetch
			{Addr: 0x02EF, Data: 0x20, Write: false}, // T1: fetch offset
			{Addr: 0x02F0, Data: 0x00, Write: false}, // T2: dummy read, compute new PC (page crossed)
			{Addr: 0x0210, Data: 0x00, Write: false}, // T3: dummy read at wrong address, fix PCH
		},
		final: cpuState{PC: 0x0310, Status: 0x02},
	},
	{
		name:    "SBC ($20),Y no page cross ($F1)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xF1, // SBC (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0x00, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $0300
			0x0301: 0x05, // operand subtracted from A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0x00, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0301, Data: 0x05, Write: false}, // T4: read operand, subtract with borrow
		},
		final: cpuState{PC: 0x0202, A: 0x0B, Y: 0x01, Status: 0x01},
	},
	{
		name:    "SBC ($20),Y page cross ($F1)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xF1, // SBC (zp),Y
			0x0201: 0x20, // pointer address (IAL)
			0x0020: 0xFF, // effective address low byte (BAL)
			0x0021: 0x03, // effective address high byte (BAH) => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand subtracted from A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF1, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x20, Write: false}, // T1: fetch pointer (IAL)
			{Addr: 0x0020, Data: 0xFF, Write: false}, // T2: fetch effective address low
			{Addr: 0x0021, Data: 0x03, Write: false}, // T3: fetch effective address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T4: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T5: read operand at fixed address, subtract with borrow
		},
		final: cpuState{PC: 0x0202, A: 0x0B, Y: 0x01, Status: 0x01},
	},
	{
		name:    "SBC $10,X ($F5)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x05, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xF5, // SBC zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0x05, // operand subtracted from A, at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF5, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0x05, Write: false}, // T3: read operand, subtract with borrow
		},
		final: cpuState{PC: 0x0202, A: 0x0B, X: 0x05, Status: 0x01},
	},
	{
		name:    "INC $10,X ($F6)",
		initial: cpuState{PC: 0x0200, X: 0x05},
		ram: map[uint16]uint8{
			0x0200: 0xF6, // INC zp,X
			0x0201: 0x10, // zero page base address (BAL)
			0x0010: 0x99, // dummy read at BAL, discarded
			0x0015: 0xFF, // old value at BAL+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF6, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: fetch zp base address
			{Addr: 0x0010, Data: 0x99, Write: false}, // T2: dummy read at BAL
			{Addr: 0x0015, Data: 0xFF, Write: false}, // T3: read old value
			{Addr: 0x0015, Data: 0xFF, Write: true},  // T4: dummy write-back of old value
			{Addr: 0x0015, Data: 0x00, Write: true},  // T5: write incremented value
		},
		final:    cpuState{PC: 0x0202, X: 0x05, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x0015: 0x00},
	},
	{
		name:    "SED ($F8)",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xF8, // SED
			0x0201: 0x99, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF8, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x99, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201, Status: 0x08}, // Decimal mode set
	},
	{
		name:    "SBC $02FF,Y no page cross ($F9)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xF9, // SBC abs,Y
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x05, // operand subtracted from A, at base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0301, Data: 0x05, Write: false}, // T3: read operand, subtract with borrow
		},
		final: cpuState{PC: 0x0203, A: 0x0B, Y: 0x01, Status: 0x01},
	},
	{
		name:    "SBC $03FF,Y page cross ($F9)",
		initial: cpuState{PC: 0x0200, A: 0x10, Y: 0x01, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xF9, // SBC abs,Y
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand subtracted from A, at corrected address base+Y
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xF9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add Y
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T4: read operand at fixed address, subtract with borrow
		},
		final: cpuState{PC: 0x0203, A: 0x0B, Y: 0x01, Status: 0x01},
	},
	{
		name:    "SBC $02FF,X no page cross ($FD)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x01, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xFD, // SBC abs,X
			0x0201: 0x00, // address low byte
			0x0202: 0x03, // address high byte => base $0300
			0x0301: 0x05, // operand subtracted from A, at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xFD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x00, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0301, Data: 0x05, Write: false}, // T3: read operand, subtract with borrow
		},
		final: cpuState{PC: 0x0203, A: 0x0B, X: 0x01, Status: 0x01},
	},
	{
		name:    "SBC $03FF,X page cross ($FD)",
		initial: cpuState{PC: 0x0200, A: 0x10, X: 0x01, Status: 0x01}, // Carry set: no borrow
		ram: map[uint16]uint8{
			0x0200: 0xFD, // SBC abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x03, // address high byte => base $03FF
			0x0300: 0xAA, // dummy read at wrong (unfixed high byte) address, discarded
			0x0400: 0x05, // operand subtracted from A, at corrected address base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xFD, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x03, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0300, Data: 0xAA, Write: false}, // T3: dummy read at wrong address
			{Addr: 0x0400, Data: 0x05, Write: false}, // T4: read operand at fixed address, subtract with borrow
		},
		final: cpuState{PC: 0x0203, A: 0x0B, X: 0x01, Status: 0x01},
	},
	{
		name:    "INC $02FF,X ($FE)",
		initial: cpuState{PC: 0x0200, X: 0x02},
		ram: map[uint16]uint8{
			0x0200: 0xFE, // INC abs,X
			0x0201: 0xFF, // address low byte
			0x0202: 0x02, // address high byte => base $02FF
			0x0301: 0xFF, // old value at base+X
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xFE, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T1: fetch address low
			{Addr: 0x0202, Data: 0x02, Write: false}, // T2: fetch address high, add X
			{Addr: 0x0201, Data: 0xFF, Write: false}, // T3: dummy read at wrong address (reuses byte at $0201)
			{Addr: 0x0301, Data: 0xFF, Write: false}, // T4: read old value at fixed address
			{Addr: 0x0301, Data: 0xFF, Write: true},  // T5: dummy write-back of old value
			{Addr: 0x0301, Data: 0x00, Write: true},  // T6: write incremented value
		},
		final:    cpuState{PC: 0x0203, X: 0x02, Status: 0x02}, // Z flag set
		finalRAM: map[uint16]uint8{0x0301: 0x00},
	},
	{
		name:    "NOP",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xEA, // NOP
			0x0201: 0x55, // dummy byte, read but discarded
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xEA, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x55, Write: false}, // T1: dummy read, no PC advance
		},
		final: cpuState{PC: 0x0201},
	},
	{
		name:    "LDA #$42",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA9, // LDA #
			0x0201: 0x42,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA9, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x42, Write: false}, // T1: operand fetch
		},
		final: cpuState{PC: 0x0202, A: 0x42, Status: 0x00},
	},
	{
		name:    "LDA #$00 sets Zero flag",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA9,
			0x0201: 0x00,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA9, Write: false},
			{Addr: 0x0201, Data: 0x00, Write: false},
		},
		final: cpuState{PC: 0x0202, A: 0x00, Status: 0x02}, // Z flag set
	},
	{
		name:    "LDA #$80 sets Negative flag",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA9,
			0x0201: 0x80,
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA9, Write: false},
			{Addr: 0x0201, Data: 0x80, Write: false},
		},
		final: cpuState{PC: 0x0202, A: 0x80, Status: 0x80}, // N flag set
	},
	{
		name:    "LDA $10",
		initial: cpuState{PC: 0x0200},
		ram: map[uint16]uint8{
			0x0200: 0xA5, // LDA zp
			0x0201: 0x10, // zero page address
			0x0010: 0x37, // value at zero page address
		},
		cycles: []busCycle{
			{Addr: 0x0200, Data: 0xA5, Write: false}, // T0: opcode fetch
			{Addr: 0x0201, Data: 0x10, Write: false}, // T1: zp address fetch
			{Addr: 0x0010, Data: 0x37, Write: false}, // T2: data read
		},
		final: cpuState{PC: 0x0202, A: 0x37, Status: 0x00},
	},
}

func TestInstructions(t *testing.T) {
	for _, tc := range instrTests {
		t.Run(tc.name, func(t *testing.T) {
			runInstrTest(t, tc)
		})
	}
}

// TestCPUStallsWhenAECLow verifies the CPU is fully disconnected from the
// bus (no state change at all) whenever the VIC-II owns Phi2.
func TestCPUStallsWhenAECLow(t *testing.T) {
	ram = [65536]byte{}
	bus = Bus{}
	cpu = CPU{}
	cpu.PortDDR = 0xFF // LORAM/HIRAM/CHAREN driven low: PLA maps plain RAM everywhere

	cpu.PC = 0x0200
	ram[0x0200] = 0xEA

	vic.BA = false
	vic.AEC = false

	before := cpu

	cpu.TickPhi2()

	// Clock legitimately advances even while AEC is low: the Phi2 clock
	// itself doesn't stop just because the CPU's bus access is stalled.
	before.Clock = cpu.Clock

	if cpu != before {
		t.Errorf("CPU state changed while AEC was low: got %+v, want %+v", cpu, before)
	}
}

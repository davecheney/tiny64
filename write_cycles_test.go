package tiny64

import (
	"strings"
	"testing"
)

func deriveWriteMask(opcode, op1, op2, x, y uint8) (mask uint16, ok bool) {
	// Unimplemented illegal opcodes panic rather than execute.
	defer func() {
		if r := recover(); r != nil {
			if s, isString := r.(string); isString && strings.HasPrefix(s, "Unhandled opcode in T1: ") {
				ok = false
			} else {
				panic(r)
			}
		}
	}()

	ram = [65536]byte{}
	bus = Bus{}
	cpu = CPU{}
	cpu.PortDDR = 0xFF

	cpu.PC = 0x1000
	cpu.SP = 0xFF
	cpu.X = x
	cpu.Y = y
	ram[0x1000] = opcode
	ram[0x1001] = op1
	ram[0x1002] = op2
	// Point any indirect vectors somewhere harmless.
	ram[0x0010], ram[0x0011] = 0x00, 0x30
	ram[0x0020], ram[0x0021] = 0x00, 0x30

	for i := 0; i < 12; i++ {
		ts := cpu.TState
		cpu.TickPhi2()
		if !bus.RW { // RW low = write cycle
			mask |= 1 << ts
		}
		if cpu.TState == 0 && i > 0 {
			break
		}
	}
	return mask, true
}

func TestCPUWriteCyclesMatchesMicrocode(t *testing.T) {
	// Other tests in this package share, and do not themselves reset, the
	// ram/bus/cpu/vic globals, so put back exactly what we found. In
	// particular, forcing BA and AEC high here would un-stall a global CPU
	// that another test left parked mid-instruction.
	savedRAM, savedBus, savedCPU, savedVIC := ram, bus, cpu, vic
	t.Cleanup(func() { ram, bus, cpu, vic = savedRAM, savedBus, savedCPU, savedVIC })

	// A store's write TState must not depend on its operands, so sweep a
	// few index/operand combinations and take the union. $81 (STA (zp,X))
	// genuinely varies, because X selects which zero page vector is read
	// and only some of them are initialised above.
	operands := [][4]uint8{
		{0x10, 0x30, 0x00, 0x00},
		{0x10, 0x30, 0x01, 0x01},
		{0xF0, 0x30, 0x04, 0x04},
		{0x20, 0x31, 0x10, 0x10},
	}

	for op := 0; op < 256; op++ {
		// Keep the observed mask wide enough for every possible T-state.
		var got uint16
		for _, v := range operands {
			m, ok := deriveWriteMask(uint8(op), v[0], v[1], v[2], v[3])
			if !ok {
				continue
			}
			got |= m
		}
		for ts := 0; ts < 256; ts++ {
			if want := got>>ts&1 != 0; cpuWritesThisCycle(uint8(op), uint8(ts)) != want {
				t.Errorf("opcode $%02X T%d: write=%v, want %v (observed mask $%04X)",
					op, ts, !want, want, got)
			}
		}
	}
}

func TestCPUWriteCyclesNeverIncludesTState0(t *testing.T) {
	for op := 0; op < 256; op++ {
		if cpuWritesThisCycle(uint8(op), 0) {
			t.Errorf("opcode $%02X claims to write on TState 0", op)
		}
	}
}

func TestCPUWriteCyclesMatchesDecoder(t *testing.T) {
	for op := range 256 {
		for ts := range 256 {
			want := cpuWritesThisCycle(uint8(op), uint8(ts))
			if got := ts < 8 && cpuWriteCycles[op]>>ts&1 != 0; got != want {
				t.Errorf("opcode $%02X T%d: table=%v, decoder=%v", op, ts, got, want)
			}
		}
	}
}

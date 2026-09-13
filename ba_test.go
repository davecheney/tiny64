package tiny64

import "testing"

// deriveWriteMask executes one instruction in isolation and returns a
// bitmask of the TStates during which the CPU drove a write cycle onto the
// bus, as observed on the bus itself rather than inferred from the opcode.
//
// The caller is responsible for saving and restoring the package level ram,
// bus and cpu globals that this clobbers.
func deriveWriteMask(opcode, op1, op2, x, y uint8) (mask uint16, ok bool) {
	// Unimplemented illegal opcodes panic rather than execute.
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()

	ram = [65536]byte{}
	bus = Bus{}
	cpu = CPU{}
	cpu.PortDDR = 0xFF
	vic.BA = true
	vic.AEC = true

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

// TestCPUWriteCyclesMatchesMicrocode re-derives cpuWriteCycles from the CPU
// core and fails if the baked table has drifted. TickPhi2 consults that
// table to decide whether the cycle it is about to run is a read, and so
// whether BA low should halt the CPU; if the two disagree the CPU will
// stall on write cycles (or fail to stall on reads) and every piece of
// cycle-exact raster code will drift.
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
		var got uint16
		for _, v := range operands {
			m, ok := deriveWriteMask(uint8(op), v[0], v[1], v[2], v[3])
			if !ok {
				continue
			}
			got |= m
		}
		if got != cpuWriteCycles[op] {
			t.Errorf("opcode $%02X: microcode writes on TStates $%04X, cpuWriteCycles has $%04X",
				op, got, cpuWriteCycles[op])
		}
	}
}

// TestCPUWriteCyclesNeverIncludesTState0 guards the assumption that lets
// TickPhi2 index cpuWriteCycles with c.Opcode. At TState 0 that field still
// holds the *previous* instruction's opcode, which is only harmless because
// TState 0 is always an opcode fetch, and so never a write, for every
// opcode in the table.
func TestCPUWriteCyclesNeverIncludesTState0(t *testing.T) {
	for op, mask := range cpuWriteCycles {
		if mask&1 != 0 {
			t.Errorf("opcode $%02X claims to write on TState 0", op)
		}
	}
}

// TestSpriteBASlotMask checks the transcription of the sprite BA windows
// from VICE x64sc's 6569 cycle table. Sprite N holds BA low from slot 44+2N
// through 48+2N: two cycles of pointer and data fetch, preceded by three
// cycles of lead time for the CPU to retire in-flight writes.
func TestSpriteBASlotMask(t *testing.T) {
	var want [CyclesPerLine]uint8
	for n := uint16(0); n < 8; n++ {
		for slot := 44 + 2*n; slot <= 48+2*n; slot++ {
			if slot >= CyclesPerLine {
				t.Fatalf("sprite %d BA window runs past end of line at slot %d", n, slot)
			}
			want[slot] |= 1 << n
		}
	}
	if want != spriteBASlotMask {
		t.Errorf("spriteBASlotMask mismatch\n got %v\nwant %v", spriteBASlotMask, want)
	}
}

package tiny64

import (
	"strings"
	"testing"
)

// deriveWriteMask executes one instruction in isolation and returns a
// bitmask of the TStates during which the CPU drove a write cycle onto the
// bus, as observed on the bus itself rather than inferred from the opcode.
//
// The caller is responsible for saving and restoring the package level ram,
// bus and cpu globals that this clobbers.
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
	vic.BA = true

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

// Compare the cold-path decoder against bus operations from the microcode,
// which does not consult it while BA is high.
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

// At T0 Opcode still names the preceding instruction.
func TestCPUWriteCyclesNeverIncludesTState0(t *testing.T) {
	for op := 0; op < 256; op++ {
		if cpuWritesThisCycle(uint8(op), 0) {
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

// TestCPUReadHoldsPreserveMicrocodeState is the general form of the test
// below: for every implemented opcode, hold each of its read cycles with BA
// and check that three held cycles leave the CPU byte for byte as they found
// it, and leave the bus untouched.
func TestCPUReadHoldsPreserveMicrocodeState(t *testing.T) {
	saveMachine(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	for op := 0; op < 256; op++ {
		mask, ok := deriveWriteMask(uint8(op), 0xF0, 0x30, 0x20, 0x20)
		if !ok {
			continue
		}
		cpu = CPU{PC: 0x1000, SP: 0xFF, X: 0x20, Y: 0x20, PortDDR: 0xFF}
		for n := 0; n < 12; n++ {
			if mask>>cpu.TState&1 == 0 {
				before, beforeBus := cpu, bus
				vic.BA = false
				for held := 0; held < 3; held++ {
					cpu.TickPhi2()
				}
				want := before
				if want.TState == 1 {
					switch want.Opcode {
					case 0x58:
						want.regP &^= P_INTERRUPT
					case 0x78:
						want.regP |= P_INTERRUPT
					}
				}
				if cpu != want || bus != beforeBus {
					t.Fatalf("opcode %02X T%d: held read changed microcode state or performed a bus access", op, before.TState)
				}
				cpu = before
				vic.BA = true
			}
			cpu.TickPhi2()
			if cpu.TState == 0 && n > 0 {
				break
			}
		}
	}
}

// TestCPUStallsOnReadCyclesOnlyWhileBALow verifies the behaviour that
// cpuWritesThisCycle exists to support: BA low drives the 6510's RDY pin, which
// halts the processor on a read cycle but lets a write cycle complete.
//
// Stalling unconditionally instead halts the CPU up to three cycles early,
// and how early depends on whichever instruction happens to be executing.
// That is what made sprite multiplexers jitter from frame to frame.
func TestCPUStallsOnReadCyclesOnlyWhileBALow(t *testing.T) {
	saveMachine(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })

	// STA $0400: three read cycles then one write cycle.
	const opcode = 0x8D
	const writeMask = 1 << 3

	setup := func() {
		ram = [65536]byte{}
		bus = Bus{}
		cpu = CPU{}
		cpu.PortDDR = 0xFF
		cpu.PC = 0x1000
		cpu.SP = 0xFF
		cpu.A = 0x42
		ram[0x1000], ram[0x1001], ram[0x1002] = opcode, 0x00, 0x04
		vic.BA = true
	}

	// Assert BA at the write cycle, while AEC still allows CPU accesses.
	setup()
	for i := 0; ; i++ {
		if i > 12 {
			t.Fatalf("never reached the write cycle of $%02X", opcode)
		}
		if cpu.Opcode == opcode && writeMask>>cpu.TState&1 == 1 {
			break
		}
		cpu.TickPhi2()
	}
	before := cpu.TState
	vic.BA = false
	cpu.TickPhi2()
	if cpu.TState == before {
		t.Errorf("CPU stalled on a write cycle (TState stuck at %d); writes ignore RDY", before)
	}
	if ram[0x0400] != 0x42 {
		t.Errorf("ram[$0400] = $%02X, want $42; the write cycle did not complete", ram[0x0400])
	}

	// Now the converse: a read cycle during the warning must not advance.
	setup()
	for i := 0; ; i++ {
		if i > 12 {
			t.Fatalf("never reached a read cycle of $%02X", opcode)
		}
		if cpu.Opcode == opcode && cpu.TState != 0 && writeMask>>cpu.TState&1 == 0 {
			break
		}
		cpu.TickPhi2()
	}
	before = cpu.TState
	beforePC := cpu.PC
	vic.BA = false
	cpu.TickPhi2()
	if cpu.TState != before || cpu.PC != beforePC {
		t.Errorf("CPU advanced through a read cycle while BA was low: TState %d->%d, PC $%04X->$%04X",
			before, cpu.TState, beforePC, cpu.PC)
	}
}

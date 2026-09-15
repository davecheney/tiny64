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

// TestCPUStallsOnReadCyclesOnlyWhileBALow verifies BA/RDY behaviour: BA low
// halts the processor on a read cycle but lets a write cycle complete.
//
// Stalling unconditionally instead halts the CPU up to three cycles early,
// and how early depends on whichever instruction happens to be executing.
// That is what made sprite multiplexers jitter from frame to frame.
func TestCPUStallsOnReadCyclesOnlyWhileBALow(t *testing.T) {
	saveRAM, saveBus, saveCPU, saveVIC := ram, bus, cpu, vic
	t.Cleanup(func() { ram, bus, cpu, vic = saveRAM, saveBus, saveCPU, saveVIC })

	// STA $0400: three read cycles then one write cycle.
	const opcode = 0x8D

	setup := func() {
		ram = [65536]byte{}
		bus = Bus{}
		cpu = CPU{}
		cpu.PortDDR = 0xFF
		cpu.PC = 0x1000
		cpu.SP = 0xFF
		cpu.A = 0x42
		ram[0x1000], ram[0x1001], ram[0x1002] = opcode, 0x00, 0x04
		vic.BA, vic.AEC = true, true
	}

	// Run up to the write cycle with the bus free, then take it away. The
	// write must still land and the CPU must move on.
	setup()
	for i := 0; ; i++ {
		if i > 12 {
			t.Fatalf("never reached the write cycle of $%02X", opcode)
		}
		if cpu.Opcode == opcode && cpu.TState == 3 {
			break
		}
		cpu.TickPhi2()
	}
	before := cpu.TState
	vic.BA = false
	vic.AEC = true
	cpu.TickPhi2()
	if cpu.TState == before {
		t.Errorf("CPU stalled on a write cycle (TState stuck at %d); writes ignore RDY", before)
	}
	if ram[0x0400] != 0x42 {
		t.Errorf("ram[$0400] = $%02X, want $42; the write cycle did not complete", ram[0x0400])
	}

	// Now the converse: a read cycle with the bus taken must not advance.
	setup()
	for i := 0; ; i++ {
		if i > 12 {
			t.Fatalf("never reached a read cycle of $%02X", opcode)
		}
		if cpu.Opcode == opcode && cpu.TState == 1 {
			break
		}
		cpu.TickPhi2()
	}
	before = cpu.TState
	beforePC := cpu.PC
	vic.BA = false
	vic.AEC = true
	cpu.TickPhi2()
	if cpu.TState != before || cpu.PC != beforePC {
		t.Errorf("CPU advanced through a read cycle while BA was low: TState %d->%d, PC $%04X->$%04X",
			before, cpu.TState, beforePC, cpu.PC)
	}
}

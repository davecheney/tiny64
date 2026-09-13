package tiny64

import "testing"

func TestIRQRecognizedAtNextInstructionBoundary(t *testing.T) {
	saveMachine(t)

	ram = [65536]byte{}
	bus = Bus{}
	cpu = CPU{
		Opcode:     0xEA,
		TState:     1,
		PC:         0x0201,
		PortDDR:    0xFF,
		effectiveI: 0,
	}
	cia1 = CIA{}
	cia2 = CIA{}
	vic = VICII{BA: true, AEC: true, IRQ: true}
	ram[0x0201] = 0xEA

	cpu.TickPhi2()
	if cpu.TState != 0 {
		t.Fatalf("TState after completing interrupted NOP = %d, want 0", cpu.TState)
	}

	cpu.TickPhi2()
	if cpu.Interrupt != 1 || cpu.Opcode != 0x00 || cpu.TState != 1 {
		t.Fatalf("IRQ was not recognized at the next instruction boundary: interrupt=%d opcode=$%02X TState=%d",
			cpu.Interrupt, cpu.Opcode, cpu.TState)
	}
	if cpu.PC != 0x0201 {
		t.Fatalf("PC = $%04X after IRQ recognition, want $0201", cpu.PC)
	}
}

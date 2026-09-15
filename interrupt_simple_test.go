package tiny64

import "testing"

func TestInterruptAtNextFetch(t *testing.T) {
	for _, source := range []string{"IRQ", "CIA2", "RESTORE"} {
		t.Run(source, func(t *testing.T) {
			newNMIFixture(t)
			cpu.TickPhi2() // Start JMP before asserting the interrupt.
			switch source {
			case "IRQ":
				cia1.IRQ = true
			case "CIA2":
				cia2.IRQ = true
			case "RESTORE":
				keyboard.Restore()
			}
			runCycles(2)
			if cpu.Interrupt != 0 || cpu.TState != 0 || cpu.PC != 0x0200 {
				t.Fatal("interrupt preempted the instruction")
			}
			vic.AEC = false
			before := cpu
			runCycles(1024)
			if cpu != before {
				t.Fatal("stalled CPU changed interrupt state")
			}
			vic.AEC = true
			cpu.TickPhi2()
			want := uint8(2)
			if source == "IRQ" {
				want = 1
			}
			if cpu.Interrupt != want || cpu.TState != 1 || cpu.PC != 0x0200 {
				t.Fatalf("next fetch: entry=%d T=%d PC=%04x", cpu.Interrupt, cpu.TState, cpu.PC)
			}
		})
	}
}

func TestIRQUsesCurrentMask(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   []byte
		status uint8
		stack  uint8
		cycles int
		want   uint8
	}{
		{"CLI", []byte{0x58}, P_INTERRUPT, 0, 2, 1},
		{"SEI", []byte{0x78}, 0, 0, 2, 0},
		{"PLP clears I", []byte{0x28}, P_INTERRUPT, 0, 4, 1},
		{"PLP sets I", []byte{0x28}, 0, P_INTERRUPT, 4, 0},
		{"RTI clears I", []byte{0x40}, P_INTERRUPT, 0, 6, 1},
		{"RTI sets I", []byte{0x40}, 0, P_INTERRUPT, 6, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			newNMIFixture(t)
			copy(ram[0x0200:], tc.code)
			cpu.regP = tc.status
			cpu.SP = 0xFC
			ram[0x01FD] = tc.stack
			ram[0x01FE], ram[0x01FF] = 0x00, 0x02
			cpu.TickPhi2()
			cia1.IRQ = true
			runCycles(tc.cycles - 1)
			if cpu.TState != 0 {
				t.Fatal("instruction did not finish")
			}
			cpu.TickPhi2()
			if cpu.Interrupt != tc.want {
				t.Fatalf("entry=%d want=%d", cpu.Interrupt, tc.want)
			}
		})
	}
}

func TestIRQHeldMaskedThenAcknowledged(t *testing.T) {
	newNMIFixture(t)
	cpu.regP = P_INTERRUPT
	cia1.IRQ = true
	runCycles(10000)
	if ram[0x11] != 0 {
		t.Fatal("masked IRQ was serviced")
	}
	cpu.regP &^= P_INTERRUPT
	for cpu.TState != 0 {
		cpu.TickPhi2()
	}
	cpu.TickPhi2()
	if cpu.Interrupt != 1 {
		t.Fatal("held IRQ was not serviced at the first unmasked fetch")
	}
	cia1.Load(0xD)
	runCycles(100)
	if ram[0x11] != 1 {
		t.Fatalf("acknowledged IRQ count=%d want=1", ram[0x11])
	}
}

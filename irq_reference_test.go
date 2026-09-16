package tiny64

import "testing"

type irqReferenceFetch struct {
	Cycle           int
	CompletionCycle int
	Address         uint16
	IRQ             bool
}

type irqReferenceCase struct {
	Name              string
	ProgramAddress    uint16
	Program           []byte
	InitialI          byte
	InitialZ          byte
	StackStatus       byte
	IRQAssertedCycles []int
	ReadHoldStart     int
	CheckOpcodeFetch  bool
	Expected          []irqReferenceFetch
	IAfterCycle       []byte
}

func TestIRQTransistorReference(t *testing.T) {
	// Original schedules checked against Visual6502 revision
	// d8ecc129b34e0eaf320e0400fcf33329475bdb1e.
	// Cycles count physical Phi2 attempts from the initial opcode fetch.
	// Each hold rejects six read attempts; I is observed on the trailing Phi1.
	cases := []irqReferenceCase{
		{Name: "nop-terminal-interior", Program: []byte{234}, IRQAssertedCycles: []int{2, 3}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, true}}},
		{Name: "nop-terminal-first", Program: []byte{234}, IRQAssertedCycles: []int{0, 1}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, true}}},
		{Name: "nop-terminal-completing", Program: []byte{234}, IRQAssertedCycles: []int{6, 7}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, true}}},
		{Name: "nop-terminal-too-late", Program: []byte{234}, IRQAssertedCycles: []int{7, 8}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, false}, {10, 10, 770, true}}},
		{Name: "lda-terminal-interior", Program: []byte{173, 0, 64}, IRQAssertedCycles: []int{4, 5}, ReadHoldStart: 3, Expected: []irqReferenceFetch{{10, 10, 771, true}}},
		{Name: "lda-operand-lost", Program: []byte{173, 0, 64}, IRQAssertedCycles: []int{2, 3}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{10, 10, 771, false}}},
		{Name: "held-opcode-pulse-lost", Program: []byte{234}, IRQAssertedCycles: []int{1, 2}, ReadHoldStart: 0, CheckOpcodeFetch: true, Expected: []irqReferenceFetch{{8, 8, 769, false}}},
		{Name: "held-opcode-level-waits", Program: []byte{234}, IRQAssertedCycles: []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11}, ReadHoldStart: 0, CheckOpcodeFetch: true, Expected: []irqReferenceFetch{{8, 8, 769, true}}},
		{Name: "accepted-before-held-successor-fetch", ProgramAddress: 519, Program: []byte{76, 0, 3}, IRQAssertedCycles: []int{1, 2, 3}, ReadHoldStart: 3, Expected: []irqReferenceFetch{{3, 9, 768, true}}},
		{Name: "cli-first-window", Program: []byte{88}, InitialI: 1, IRQAssertedCycles: []int{0, 1}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, true}}, IAfterCycle: []byte{1, 0, 0, 0, 0, 0, 0, 0, 0}},
		{Name: "cli-late-window", Program: []byte{88}, InitialI: 1, IRQAssertedCycles: []int{2, 3}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, true}}, IAfterCycle: []byte{1, 0, 0, 0, 0, 0, 0, 0, 0}},
		{Name: "sei-first-window", Program: []byte{120}, IRQAssertedCycles: []int{0, 1}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, true}}, IAfterCycle: []byte{0, 1, 1, 1, 1, 1, 1, 1, 1}},
		{Name: "sei-late-window", Program: []byte{120}, IRQAssertedCycles: []int{1, 2}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 769, false}}, IAfterCycle: []byte{0, 1, 1, 1, 1, 1, 1, 1, 1}},
		{Name: "plp-clear-retains-old-mask", Program: []byte{40}, InitialI: 1, IRQAssertedCycles: []int{4, 5}, ReadHoldStart: 3, Expected: []irqReferenceFetch{{10, 10, 769, false}}, IAfterCycle: []byte{1, 1, 1, 1, 1, 1, 1, 1, 1, 0, 0}},
		{Name: "plp-set-retains-acceptance", Program: []byte{40}, StackStatus: 36, IRQAssertedCycles: []int{4, 5}, ReadHoldStart: 3, Expected: []irqReferenceFetch{{10, 10, 769, true}}, IAfterCycle: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1}},
		{Name: "branch-taken-operand", Program: []byte{208, 2}, IRQAssertedCycles: []int{2, 3}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{9, 9, 772, true}}},
		{Name: "branch-not-taken-operand", Program: []byte{208, 2}, InitialZ: 1, IRQAssertedCycles: []int{2, 3}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{8, 8, 770, true}}},
		{Name: "branch-same-page-final-lost", Program: []byte{208, 2}, IRQAssertedCycles: []int{3, 4}, ReadHoldStart: 2, Expected: []irqReferenceFetch{{9, 9, 772, false}}},
		{Name: "branch-same-page-final-level", Program: []byte{208, 2}, IRQAssertedCycles: []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}, ReadHoldStart: 2, Expected: []irqReferenceFetch{{9, 9, 772, false}, {11, 11, 773, true}}},
		{Name: "branch-cross-operand", ProgramAddress: 12541, Program: []byte{208, 2}, IRQAssertedCycles: []int{2, 3}, ReadHoldStart: 1, Expected: []irqReferenceFetch{{10, 10, 12545, true}}},
		{Name: "branch-cross-t3-lost", ProgramAddress: 12541, Program: []byte{208, 2}, IRQAssertedCycles: []int{3, 4}, ReadHoldStart: 2, Expected: []irqReferenceFetch{{10, 10, 12545, false}}},
		{Name: "branch-cross-t3-level", ProgramAddress: 12541, Program: []byte{208, 2}, IRQAssertedCycles: []int{3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13}, ReadHoldStart: 2, Expected: []irqReferenceFetch{{10, 10, 12545, true}}},
		{Name: "branch-cross-terminal", ProgramAddress: 12541, Program: []byte{208, 2}, IRQAssertedCycles: []int{4, 5}, ReadHoldStart: 3, Expected: []irqReferenceFetch{{10, 10, 12545, true}}},
		{Name: "cli-successor-fetch-pulse-lost", Program: []byte{88}, InitialI: 1, IRQAssertedCycles: []int{3, 4}, ReadHoldStart: 2, Expected: []irqReferenceFetch{{2, 8, 769, false}}, IAfterCycle: []byte{1, 0, 0, 0, 0, 0, 0, 0, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			if len(tc.Program) == 0 || tc.InitialI > 1 || tc.InitialZ > 1 ||
				tc.ReadHoldStart < 0 || len(tc.Expected) == 0 {
				t.Fatal("invalid or unsupported IRQ reference case")
			}
			if tc.ProgramAddress == 0 {
				tc.ProgramAddress = 0x0300
			}
			if tc.StackStatus == 0 {
				tc.StackStatus = P_UNUSED
			}
			const irqVector = 0x8000
			c := newIRQTestCPU(t, "6510")
			for i := range ram {
				ram[i] = 0xEA
			}
			// The reference establishes Z by loading A with 1 or 0.
			cpu.PC, cpu.SP, cpu.A, cpu.X = tc.ProgramAddress, 0xFE, 1-tc.InitialZ, 0xFE
			cpu.regP = P_UNUSED | tc.InitialI*P_INTERRUPT | tc.InitialZ*P_ZERO
			c.program(0x4000, 66)
			c.program(0x01FF, tc.StackStatus)
			c.program(tc.ProgramAddress, tc.Program...)
			c.program(0xFFFE, 0x00, 0x80)
			c.program(irqVector, 0x4C, 0x00, 0x80)
			wanted := tc.Expected
			lastCycle := wanted[len(wanted)-1].CompletionCycle + 16
			asserted := make([]bool, lastCycle+1)
			for _, cycle := range tc.IRQAssertedCycles {
				if cycle < 0 || cycle > lastCycle {
					t.Fatalf("IRQ cycle %d outside observation window", cycle)
				}
				asserted[cycle] = true
			}
			var fetches []irqReferenceFetch
			var writes []busCycle
			fetching, vectorLow, vectorHigh := false, false, false
			for cycle := 0; cycle <= lastCycle; cycle++ {
				held := cycle >= tc.ReadHoldStart && cycle < tc.ReadHoldStart+6
				vic.BA = !held
				c.pin(asserted[cycle], false)
				before := cpu.TState
				if before == 0 && !fetching {
					fetches = append(fetches, irqReferenceFetch{Cycle: cycle, Address: cpu.PC})
					fetching = true
				}
				c.tick()
				if before == 0 && cpu.TState != 0 {
					f := &fetches[len(fetches)-1]
					f.CompletionCycle, f.IRQ = cycle, cpu.Interrupt == 1
					fetching = false
				}
				if !held {
					if !bus.RW {
						writes = append(writes, c.bus())
					} else {
						vectorLow = vectorLow || bus.Address == 0xFFFE
						vectorHigh = vectorHigh || bus.Address == 0xFFFF
					}
				}
				if cycle < len(tc.IAfterCycle) {
					if got := (cpu.regP & P_INTERRUPT) / P_INTERRUPT; got != tc.IAfterCycle[cycle] {
						t.Fatalf("cycle %d: architectural I=%d, want %d", cycle, got, tc.IAfterCycle[cycle])
					}
				}
			}
			if len(fetches) < len(wanted)+1 {
				t.Fatalf("only %d fetches, want at least %d", len(fetches), len(wanted)+1)
			}
			for i, want := range wanted {
				if got := fetches[i+1]; got != want {
					t.Fatalf("following fetch %d: got %+v, want %+v", i+1, got, want)
				}
			}
			if tc.CheckOpcodeFetch && fetches[0].IRQ {
				t.Fatal("initial opcode fetch unexpectedly accepted IRQ")
			}
			expectsEntry := false
			for _, f := range wanted {
				expectsEntry = expectsEntry || f.IRQ
			}
			if expectsEntry {
				if len(writes) != 3 || writes[2].Data&P_BREAK != 0 || !vectorLow || !vectorHigh {
					t.Fatalf("invalid IRQ entry: writes=%+v, vector reads=%v/%v", writes, vectorLow, vectorHigh)
				}
				handler := false
				for _, f := range fetches {
					handler = handler || f.Address == irqVector
				}
				if !handler {
					t.Fatal("handler opcode was never fetched")
				}
			} else {
				if len(writes) != 0 || vectorLow || vectorHigh {
					t.Fatalf("unexpected delayed IRQ entry: writes=%+v, vector reads=%v/%v", writes, vectorLow, vectorHigh)
				}
				for _, f := range fetches {
					if f.IRQ {
						t.Fatalf("unexpected delayed IRQ at %+v", f)
					}
				}
			}
		})
	}
}

package tiny64

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
)

type irqReferenceFetch struct {
	Cycle           int
	CompletionCycle int
	Address         uint16
	IRQ             bool
}

type irqReferenceCase struct {
	Name           string
	ProgramAddress uint16
	Program        []byte
	MemoryFill     byte
	InitialI       byte
	InitialZ       byte
	InitialA       byte
	InitialX       byte
	InitialS       byte
	Memory         []struct {
		Address uint16
		Bytes   []byte
	}
	IRQVector         uint16
	Handler           []byte
	NMIAsserted       bool
	ResetAsserted     bool
	IRQAssertedCycles []int
	ReadHold          struct {
		StartCycle int
		Length     int
	}
	Expected struct {
		OpcodeFetchIRQ       *bool
		FirstFollowingFetch  *irqReferenceFetch
		SecondFollowingFetch *irqReferenceFetch
		IAfterCycle          []byte
	}
}

func TestIRQTransistorReference(t *testing.T) {
	data, err := os.ReadFile("testdata/irq/irq-rdy-cycles.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Schema   string
		Defaults json.RawMessage
		Cases    []json.RawMessage
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if fixture.Schema != "nmos6502-irq-rdy-cycles-v1" || len(fixture.Cases) != 24 {
		t.Fatalf("unexpected reference schema/count: %q/%d", fixture.Schema, len(fixture.Cases))
	}
	decode := func(data []byte, c *irqReferenceCase) {
		t.Helper()
		d := json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if err := d.Decode(c); err != nil {
			t.Fatal(err)
		}
	}
	var defaults irqReferenceCase
	decode(fixture.Defaults, &defaults)
	for _, data := range fixture.Cases {
		var tc irqReferenceCase
		decode(fixture.Defaults, &tc)
		tc.Memory = nil // Per-case patches supplement the default memory.
		decode(data, &tc)
		t.Run(tc.Name, func(t *testing.T) {
			if len(tc.Program) == 0 || tc.NMIAsserted || tc.ResetAsserted || tc.InitialI > 1 || tc.InitialZ > 1 ||
				tc.ReadHold.StartCycle < 0 || tc.ReadHold.Length < 1 || tc.Expected.FirstFollowingFetch == nil {
				t.Fatal("invalid or unsupported IRQ reference case")
			}
			c := newIRQTestCPU(t, "6510")
			for i := range ram {
				ram[i] = tc.MemoryFill
			}
			cpu.PC, cpu.SP, cpu.A, cpu.X = tc.ProgramAddress, tc.InitialS, tc.InitialA, tc.InitialX
			cpu.regP = P_UNUSED | tc.InitialI*P_INTERRUPT | tc.InitialZ*P_ZERO
			for _, m := range defaults.Memory {
				c.program(m.Address, m.Bytes...)
			}
			for _, m := range tc.Memory {
				c.program(m.Address, m.Bytes...)
			}
			c.program(tc.ProgramAddress, tc.Program...)
			c.program(0xFFFE, byte(tc.IRQVector), byte(tc.IRQVector>>8))
			c.program(tc.IRQVector, tc.Handler...)
			wanted := []*irqReferenceFetch{tc.Expected.FirstFollowingFetch}
			if tc.Expected.SecondFollowingFetch != nil {
				wanted = append(wanted, tc.Expected.SecondFollowingFetch)
			}
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
				held := cycle >= tc.ReadHold.StartCycle && cycle < tc.ReadHold.StartCycle+tc.ReadHold.Length
				vic.AEC = !held
				c.pin(asserted[cycle], false)
				before := cpu.TState
				if held && cpuWriteCycles[cpu.Opcode]>>before&1 != 0 {
					t.Fatal("reference read hold overlaps a write")
				}
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
				if cycle < len(tc.Expected.IAfterCycle) {
					if got := (cpu.regP & P_INTERRUPT) / P_INTERRUPT; got != tc.Expected.IAfterCycle[cycle] {
						t.Fatalf("cycle %d: architectural I=%d, want %d", cycle, got, tc.Expected.IAfterCycle[cycle])
					}
				}
			}
			if len(fetches) < len(wanted)+1 {
				t.Fatalf("only %d fetches, want at least %d", len(fetches), len(wanted)+1)
			}
			for i, want := range wanted {
				if got := fetches[i+1]; got != *want {
					t.Fatalf("following fetch %d: got %+v, want %+v", i+1, got, *want)
				}
			}
			if want := tc.Expected.OpcodeFetchIRQ; want != nil && fetches[0].IRQ != *want {
				t.Fatalf("initial opcode fetch IRQ=%v, want %v", fetches[0].IRQ, *want)
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
					handler = handler || f.Address == tc.IRQVector
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

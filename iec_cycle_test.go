package tiny64

import (
	"slices"
	"testing"
)

type iecATNRecorder struct {
	samples []bool
}

func (*iecATNRecorder) iecCLKOut() bool   { return false }
func (*iecATNRecorder) iecDATAOut() bool  { return false }
func (*iecATNRecorder) iecAddress() uint8 { return 8 }
func (p *iecATNRecorder) iecTick() {
	p.samples = append(p.samples, ATNAsserted())
}

func TestIECObservesCIA2WriteOnSameCycle(t *testing.T) {
	requireBusTrace(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	newMachine(t)
	iecBus = nil
	p := &iecATNRecorder{}
	attachIEC(p)
	cpu.PC = 0x0200
	cpu.A = 0x08
	cpu.PortDDR, cpu.Port = 0x07, 0x07
	copy(ram[0x0200:], []byte{0x8D, 0x00, 0xDD}) // STA $DD00
	cia2.Store(0xDD02, 0x08)
	cia2.Store(0xDD00, 0x00)

	for range 4 {
		vic.StepCycle()
	}
	if want := []bool{false, false, false, true}; !slices.Equal(p.samples, want) {
		t.Fatalf("ATN sampled inside peripheral ticks=%v, want %v", p.samples, want)
	}
	if bus.RW || bus.Address != 0xDD00 || bus.Data != 0x08 {
		t.Fatalf("final CPU bus transaction=%+v, want STA $DD00 = $08", bus)
	}
}

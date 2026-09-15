package tiny64

import "testing"

func TestTwoColorLookupMatchesDecode(t *testing.T) {
	dots := [...]func(*VICII){
		(*VICII).dotclock0, (*VICII).dotclock1,
		(*VICII).dotclock2, (*VICII).dotclock3,
		(*VICII).dotclock4, (*VICII).dotclock5,
		(*VICII).dotclock6, (*VICII).dotclock7,
	}
	v := &VICII{}
	for background := range 256 {
		v.WriteRegister(0xD021, uint8(background))
		v.Reset()
		v.rasterLine = 100
		v.syncLineVisibility()
		if v.gdColor != [2]uint8{uint8(background), 0} {
			t.Fatalf("reset palette=%v, background=%d", v.gdColor, background)
		}
		for foreground := range 256 {
			v.dot = 47
			v.videoBufferPending = uint16(foreground) << 8
			v.dotclock7()
			for bit := range 2 {
				for phase, step := range dots {
					v.dot = uint16(16 + phase)
					v.gdSequencer = uint8(bit << 7)
					step(v)
					want := uint8(background)
					if bit != 0 {
						want = uint8(foreground)
					}
					if !frameBufferPixelIs(v.dot, 100, want) {
						t.Fatalf("background=%d foreground=%d bit=%d phase=%d: pixel=%v, want colour %d",
							background, foreground, bit, phase, frameBufferPixelRGBA(v.dot, 100), want&15)
					}
					if v.gdSequencer != 0 {
						t.Fatalf("phase %d did not shift the sequencer", phase)
					}
				}
			}
		}
	}
}

func TestTwoColorLookupBackgroundCPUWrite(t *testing.T) {
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	newMachine(t)
	iecBus = nil
	cpu.PC = 0x0200
	cpu.A = 0xA3
	copy(ram[0x0200:], []byte{0x8D, 0x21, 0xD0, 0xEA}) // STA $D021; NOP
	vic.WriteRegister(0xD021, 0xB6)
	vic.rasterLine = 100
	vic.dot = 64
	vic.syncLineVisibility()

	for range 4 {
		vic.StepCycle()
	}
	if bus.RW || bus.Address != 0xD021 || bus.Data != 0xA3 {
		t.Fatalf("expected background store, got bus %+v", bus)
	}
	for dot := uint16(89); dot <= 96; dot++ {
		if !frameBufferPixelIs(dot, 100, 6) {
			t.Fatalf("dot %d changed before CPU Phi2", dot)
		}
	}
	vic.StepCycle()
	for dot := uint16(97); dot <= 104; dot++ {
		if !frameBufferPixelIs(dot, 100, 3) {
			t.Fatalf("dot %d missed the background write", dot)
		}
	}
}

func TestTwoColorLookupConsistentAcrossFrame(t *testing.T) {
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	newMachine(t)
	iecBus = nil
	cpu.PC = 0x0800
	copy(ram[0x0800:], []byte{
		0xE6, 0x20, // INC $20
		0xA5, 0x20, // LDA $20
		0x8D, 0x61, 0xD0, // STA $D061 (mirrored background register)
		0x4C, 0x00, 0x08, // JMP $0800
	})
	vic.WriteRegister(0xD011, 0x1B)
	vic.WriteRegister(0xD016, 0x08)
	vic.WriteRegister(0xD018, 0x14)
	for i := range 1000 {
		ram[0x0400+i] = byte(i)
		colorRAM[i] = byte(i & 15)
	}
	for cycle := range CyclesPerFrame {
		vic.StepCycle()
		want := [2]uint8{vic.background0, byte(vic.videoBuffer >> 8)}
		if vic.gdColor != want {
			t.Fatalf("cycle %d: palette=%v, want %v", cycle, vic.gdColor, want)
		}
	}
}

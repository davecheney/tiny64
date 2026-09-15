package tiny64

import "testing"

func TestCIAMaskWriteInterruptEntry(t *testing.T) {
	for _, nmi := range []bool{false, true} {
		t.Run(map[bool]string{false: "IRQ", true: "NMI"}[nmi], func(t *testing.T) {
			newNMIFixture(t)
			cpu.Port = 6
			cpu.A = 0x81
			chip, page, interrupt := &cia1, byte(0xDC), uint8(1)
			if nmi {
				chip, page, interrupt = &cia2, 0xDD, 2
			}
			chip.Store(4, 1)
			chip.Store(5, 0)
			chip.Store(0xE, 0x09)
			chip.Tick() // Latch a masked flag, then stop the timer.
			copy(ram[0x0200:], []byte{0x8D, 0x0D, page, 0xEA, 0xEA})
			for cycle := 1; cycle <= 5; cycle++ {
				cpu.TickPhi2()
				ciaTick()
				if cycle < 5 && cpu.Interrupt != 0 {
					t.Fatalf("interrupt entered early at cycle %d", cycle)
				}
			}
			if cpu.Interrupt != interrupt || cpu.TState != 1 || cpu.PC != 0x0203 {
				t.Fatalf("entry: interrupt=%d T=%d PC=%04x", cpu.Interrupt, cpu.TState, cpu.PC)
			}
		})
	}
}

func TestCIAHoistTimerSources(t *testing.T) {
	for _, timerB := range []bool{false, true} {
		for _, masked := range []bool{false, true} {
			c := &CIA{}
			low, control, flag := uint16(4), uint16(0xE), byte(1)
			if timerB {
				low, control, flag = 6, 0xF, 2
			}
			if !masked {
				c.Store(0xD, 0x80|flag)
			}
			c.Store(low, 1)
			c.Store(low+1, 0)
			c.Store(control, 0x09)
			c.Tick()
			if c.IRQ == masked || c.icr&flag == 0 {
				t.Fatalf("timerB=%t masked=%t: IRQ=%t ICR=%02x", timerB, masked, c.IRQ, c.icr)
			}
			c.Load(0xD)
			for range 3 {
				c.Tick()
			}
			if c.IRQ || c.icr != 0 {
				t.Fatal("acknowledged stopped timer reasserted")
			}
		}
	}
}

func TestCIAMaskWriteUnmasksLatchedFlag(t *testing.T) {
	c := &CIA{}
	c.Store(4, 1)
	c.Store(5, 0)
	c.Store(0xE, 0x09)
	c.Tick()
	if c.IRQ {
		t.Fatal("masked underflow asserted IRQ")
	}
	c.Store(0xD, 0x81)
	if !c.IRQ || c.icr != 0x81 {
		t.Fatalf("mask write did not immediately assert: IRQ=%t ICR=%02x", c.IRQ, c.icr)
	}
	c.Store(0xD, 0x01)
	if !c.IRQ {
		t.Fatal("mask clear acknowledged a latched interrupt")
	}
	if status := c.Load(0xD); status != 0x81 || c.IRQ {
		t.Fatalf("acknowledgement: status=%02x IRQ=%t", status, c.IRQ)
	}
	c.Store(0xD, 0x81)
	c.Tick()
	if c.IRQ {
		t.Fatal("enabling an acknowledged stopped source asserted IRQ")
	}
}

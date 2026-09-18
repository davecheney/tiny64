//go:build drive1541

package tiny64

import (
	"testing"

	"github.com/davecheney/tiny64/rom"
)

// irqTestCores is the set of CPU cores the IRQ suite runs against. The
// 6502 arm is the 1541's drive CPU, so it is only here when the physical
// drive is compiled in.
var irqTestCores = []string{"6510", "6502"}

// saveIRQDriveState puts the drive's bus back after a test that drove the
// 6502 core through it.
func saveIRQDriveState(t *testing.T) {
	savedDriveBus := driveBus
	t.Cleanup(func() { driveBus = savedDriveBus })
	via1, via2 = VIA{}, VIA{}
}

// newIRQTestCPUForCore builds the 6502 arm: the 1541's CPU, its RAM, and
// its two VIAs standing in for the C64's VIC-II and CIA as interrupt
// sources.
//
// The closures it fills in are the per-core half of the peripheral-sourced
// subtests. The 1541 has no VIC-II to hang a clock tree off, so its VIAs
// are clocked at the top of DriveCPU.TickPhi2 and an underflow is sampled
// in the cycle it happens - two cycles earlier than the C64 manages, which
// is why armTimer reports a different cycle count and entry address.
func newIRQTestCPUForCore(t *testing.T, core string) irqTestCPU {
	t.Helper()
	if core != "6502" {
		t.Fatalf("unknown IRQ test core %q", core)
	}
	savedROM := rom.Drive1541
	rom.Drive1541 = make([]byte, len(savedROM))
	t.Cleanup(func() { rom.Drive1541 = savedROM })
	driveCPU = DriveCPU{PC: 0x0200, SP: 0xFF}
	driveRAM = [0x0800]byte{}
	driveBus = DriveBus{}
	via1, via2 = VIA{}, VIA{}
	via1SampleATN()
	return irqTestCPU{
		pc: &driveCPU.PC, sp: &driveCPU.SP, p: &driveCPU.regP, x: &driveCPU.X, y: &driveCPU.Y,
		opcode: &driveCPU.Opcode, tstate: &driveCPU.TState, interrupt: &driveCPU.Interrupt,
		tick: driveCPU.TickPhi2, reset: driveCPU.Reset,
		pin: func(a, b bool) {
			for i, asserted := range []bool{a, b} {
				v := []*VIA{&via2, &via1}[i]
				v.ier, v.ifr = viaIFRCA1, 0
				if asserted {
					v.ifr = viaIFRCA1
				}
				v.updateIRQ()
			}
		},
		put: func(addr uint16, b byte) {
			switch {
			case addr < 0x0800:
				driveRAM[addr] = b
			case addr >= 0xC000:
				rom.Drive1541[addr-0xC000] = b
			default:
				t.Fatalf("IRQ fixture write outside drive memory: $%04X", addr)
			}
		},
		read: driveLoad,
		bus:  func() busCycle { return busCycle{driveBus.Address, driveBus.Data, !driveBus.RW} },

		irqStatePtr: func() *irqState { return &driveCPU.irq },
		poisonBus:   func() { driveBus.Address = 0xDEAD },
		armTimer: func() (int, uint16) {
			via2 = VIA{t1c: 1, t1l: 0xFFFF, acr: 0x40, ier: 0x40}
			return 2, 0x0201
		},
		armFinalRead: func(c irqTestCPU) {
			via1.ier, via1.ifr = viaIFRCA1, viaIFRCA1
			via1.updateIRQ()
			c.program(0x0200, 0xAD, 0x01, 0x18)
		},
		finalReadAcked: func() bool { return !via1.IRQ },
		armPenultimateWrite: func(c irqTestCPU) {
			via2.ier, via2.ifr = viaIFRCA1, viaIFRCA1
			via2.updateIRQ()
			c.program(0x0200, 0xEE, 0x0D, 0x1C)
		},
		penultimateWriteAcked: func() bool { return !via2.IRQ },
	}
}

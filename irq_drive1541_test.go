//go:build drive1541

package tiny64

import (
	"fmt"
	"testing"

	"github.com/davecheney/tiny64/rom"
)

// The 1541's 6502 runs the same interrupt logic as the C64's 6510, so it
// answers the same schedules. These are the drive's copy of the twelve
// tests that apply to both cores, wired to driveCPU and the two VIAs
// instead of cpu, the CIAs and the VIC-II. They live here rather than as
// a second arm of irq_test.go so that neither file has to know the other
// core exists.

// The schedules below use bus-cycle numbering, not Visual6502's T-state
// names. See https://www.nesdev.org/wiki/CPU_interrupts and
// https://www.nesdev.org/wiki/Visual6502wiki/6502_Interrupt_Recognition_Stages_and_Tolerances.
type driveIRQTestCPU struct {
	pc                        *uint16
	sp, p, x, y               *uint8
	opcode, tstate, interrupt *uint8
	tick                      func()
	reset                     func()
	pin                       func(bool, bool)
	put                       func(uint16, byte)
	read                      func(uint16) byte
	bus                       func() busCycle
}

func newDriveIRQTestCPU(t *testing.T) driveIRQTestCPU {
	t.Helper()
	saveMachine(t)
	savedBus, savedDriveBus := bus, driveBus
	t.Cleanup(func() { bus, driveBus = savedBus, savedDriveBus })
	cpu = CPU{PC: 0x0200, SP: 0xFF, PortDDR: 0xFF}
	cia = CIA{}
	via1, via2 = VIA{}, VIA{}
	keyboard = Keyboard{}
	vic = VICII{BA: true}
	cartridge = Cartridge{}
	bus = Bus{}
	ram = [65536]byte{}

	savedROM := rom.Drive1541
	rom.Drive1541 = make([]byte, len(savedROM))
	t.Cleanup(func() { rom.Drive1541 = savedROM })
	driveCPU = DriveCPU{PC: 0x0200, SP: 0xFF}
	driveRAM = [0x0800]byte{}
	driveBus = DriveBus{}
	via1, via2 = VIA{}, VIA{}
	via1SampleATN()
	c := driveIRQTestCPU{
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
	}
	for addr := uint16(0x0200); addr < 0x0800; addr++ {
		c.put(addr, 0xEA)
	}
	c.put(0xFFFE, 0x00)
	c.put(0xFFFF, 0x04)
	c.put(0xFFFC, 0x00)
	c.put(0xFFFD, 0x02)
	return c
}

func (c driveIRQTestCPU) program(addr uint16, code ...byte) {
	for i, b := range code {
		c.put(addr+uint16(i), b)
	}
}

func (c driveIRQTestCPU) cycles(n int) {
	for range n {
		c.tick()
	}
}

func (c driveIRQTestCPU) checkEntry(t *testing.T, want bool, pc uint16) {
	t.Helper()
	c.tick()
	got := *c.interrupt == 1 && *c.opcode == 0 && *c.tstate == 1
	if got != want {
		t.Fatalf("IRQ entry = %v, want %v (PC=$%04X opcode=$%02X TState=%d)",
			got, want, *c.pc, *c.opcode, *c.tstate)
	}
	if want && *c.pc != pc {
		t.Fatalf("interrupted PC=$%04X, want $%04X", *c.pc, pc)
	}
}

func TestDriveIRQInactiveCycleSampling(t *testing.T) {
	for _, pending := range []bool{false, true} {
		for sources := range 4 {
			for _, masked := range []bool{false, true} {
				t.Run(fmt.Sprintf("pending=%t/sources=%d/masked=%t", pending, sources, masked), func(t *testing.T) {
					c := newDriveIRQTestCPU(t)
					state := &driveCPU.irq
					*state = irqState{pending: pending}
					*c.opcode, *c.tstate = 0xEA, 1 // Complete NOP without an eligible prior sample.
					if masked {
						*c.p |= P_INTERRUPT
					}
					c.pin(sources&1 != 0, sources&2 != 0)
					c.tick()
					if want := (irqState{sampled: sources != 0, pending: pending}); *state != want {
						t.Fatalf("IRQ state = %+v, want %+v", *state, want)
					}
					if *c.tstate != 0 {
						t.Fatal("NOP did not complete")
					}
				})
			}
		}
	}
}

func TestDriveIRQInstructionPulses(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   []byte
		cycles int
		pc     uint16
		setup  func(driveIRQTestCPU)
	}{
		{"NOP", []byte{0xEA}, 2, 0x0201, nil},
		{"LDA-immediate", []byte{0xA9, 0x42}, 2, 0x0202, nil},
		{"LDA-zp", []byte{0xA5, 0x10}, 3, 0x0202, nil},
		{"LDA-zp-X", []byte{0xB5, 0x10}, 4, 0x0202, nil},
		{"LDA-absolute", []byte{0xAD, 0x00, 0x03}, 4, 0x0203, nil},
		{"LDA-absolute-X", []byte{0xBD, 0x00, 0x03}, 4, 0x0203, nil},
		{"LDA-absolute-X-cross", []byte{0xBD, 0xFF, 0x03}, 5, 0x0203, func(c driveIRQTestCPU) { *c.x = 1 }},
		{"LDA-indirect-X", []byte{0xA1, 0x10}, 6, 0x0202, func(c driveIRQTestCPU) { c.put(0x11, 0x03) }},
		{"LDA-indirect-Y", []byte{0xB1, 0x10}, 5, 0x0202, func(c driveIRQTestCPU) { c.put(0x11, 0x03) }},
		{"LDA-indirect-Y-cross", []byte{0xB1, 0x10}, 6, 0x0202, func(c driveIRQTestCPU) {
			*c.y = 1
			c.put(0x10, 0xFF)
			c.put(0x11, 0x03)
		}},
		{"STA-absolute", []byte{0x8D, 0x00, 0x03}, 4, 0x0203, nil},
		{"STA-indirect-X", []byte{0x81, 0x10}, 6, 0x0202, func(c driveIRQTestCPU) { c.put(0x11, 0x03) }},
		{"INC-zp", []byte{0xE6, 0x10}, 5, 0x0202, nil},
		{"INC-absolute-X", []byte{0xFE, 0xFF, 0x03}, 7, 0x0203, func(c driveIRQTestCPU) { *c.x = 1 }},
		{"PHA", []byte{0x48}, 3, 0x0201, nil},
		{"PLA", []byte{0x68}, 4, 0x0201, nil},
		{"JMP", []byte{0x4C, 0x00, 0x05}, 3, 0x0500, nil},
		{"JMP-indirect", []byte{0x6C, 0x10, 0x00}, 5, 0x0500, func(c driveIRQTestCPU) { c.put(0x11, 0x05) }},
		{"JSR", []byte{0x20, 0x00, 0x05}, 6, 0x0500, nil},
		{"RTS", []byte{0x60}, 6, 0x0500, func(c driveIRQTestCPU) {
			*c.sp = 0xFD
			c.put(0x01FE, 0xFF)
			c.put(0x01FF, 0x04)
		}},
	} {
		for pulse := range tc.cycles {
			t.Run(fmt.Sprintf("%s/pulse-%d", tc.name, pulse), func(t *testing.T) {
				c := newDriveIRQTestCPU(t)
				c.program(0x0200, tc.code...)
				if tc.setup != nil {
					tc.setup(c)
				}
				for cycle := range tc.cycles {
					c.pin(cycle == pulse, false)
					c.tick()
				}
				c.pin(false, false)
				if *c.tstate != 0 {
					t.Fatalf("instruction not finished after %d cycles: TState=%d", tc.cycles, *c.tstate)
				}
				c.checkEntry(t, pulse == tc.cycles-2, tc.pc)
			})
		}
	}
}

func TestDriveIRQBranchPulses(t *testing.T) {
	for _, branch := range []struct{ op, mask, taken uint8 }{
		{0x10, P_SIGN, 0}, {0x30, P_SIGN, P_SIGN},
		{0x50, P_OVERFLOW, 0}, {0x70, P_OVERFLOW, P_OVERFLOW},
		{0x90, P_CARRY, 0}, {0xB0, P_CARRY, P_CARRY},
		{0xD0, P_ZERO, 0}, {0xF0, P_ZERO, P_ZERO},
	} {
		for _, tc := range []struct {
			name   string
			pc     uint16
			offset byte
			taken  bool
			cycles int
			target uint16
		}{
			{"not-taken", 0x0200, 0x05, false, 2, 0x0202},
			{"same-page", 0x0200, 0x05, true, 3, 0x0207},
			{"cross-up", 0x02FC, 0x05, true, 4, 0x0303},
			{"cross-down", 0x0300, 0xFC, true, 4, 0x02FE},
		} {
			for pulse := range tc.cycles {
				t.Run(fmt.Sprintf("%02X/%s/pulse-%d", branch.op, tc.name, pulse), func(t *testing.T) {
					c := newDriveIRQTestCPU(t)
					*c.pc, *c.p = tc.pc, branch.taken
					if !tc.taken {
						*c.p ^= branch.mask
					}
					c.program(tc.pc, branch.op, tc.offset)
					for cycle := range tc.cycles {
						c.pin(cycle == pulse, false)
						c.tick()
					}
					c.pin(false, false)
					if *c.tstate != 0 || *c.pc != tc.target {
						t.Fatalf("branch did not complete: PC=$%04X TState=%d", *c.pc, *c.tstate)
					}
					c.checkEntry(t, pulse == 0 || tc.cycles == 4 && pulse == 2, tc.target)
				})
			}
		}
	}
}

func TestDriveIRQMaskPolling(t *testing.T) {
	for _, tc := range []struct {
		name              string
		op, before, after uint8
		cycles            int
		immediate         bool
	}{
		{"CLI", 0x58, P_INTERRUPT, 0, 2, false},
		{"SEI", 0x78, 0, P_INTERRUPT, 2, true},
		{"PLP-clear", 0x28, P_INTERRUPT, 0, 4, false},
		{"PLP-set", 0x28, 0, P_INTERRUPT, 4, true},
		{"RTI-clear", 0x40, P_INTERRUPT, 0, 6, true},
		{"RTI-set", 0x40, 0, P_INTERRUPT, 6, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newDriveIRQTestCPU(t)
			*c.p, *c.sp = tc.before, 0xFC
			c.program(0x0200, tc.op)
			c.put(0x01FD, tc.after)
			c.put(0x01FE, 0x00)
			c.put(0x01FF, 0x05)
			// A pulse on the relevant penultimate cycle cannot be
			// cancelled by a later flag change or pin release.
			for cycle := range tc.cycles {
				c.pin(cycle == tc.cycles-2, false)
				c.tick()
			}
			c.pin(false, false)
			pc := uint16(0x0201)
			if tc.op == 0x40 {
				pc = 0x0500
			}
			c.checkEntry(t, tc.immediate, pc)
			if tc.immediate {
				c.cycles(4)
				got := c.read(0x0100 + uint16(*c.sp+1))
				if want := tc.after | P_UNUSED; got != want {
					t.Fatalf("stacked status=$%02X, want $%02X", got, want)
				}
			}
		})
	}
}

func TestDriveIRQEntryBusCycles(t *testing.T) {
	c := newDriveIRQTestCPU(t)
	*c.p = P_CARRY | P_BREAK
	c.pin(true, false)
	c.tick() // NOP opcode fetch: sample IRQ.
	c.pin(false, false)
	c.tick() // NOP terminal cycle: accept the preceding sample.
	want := []busCycle{
		{0x0201, 0xEA, false},
		{0x0201, 0xEA, false},
		{0x01FF, 0x02, true},
		{0x01FE, 0x01, true},
		{0x01FD, P_UNUSED | P_CARRY, true},
		{0xFFFE, 0x00, false},
		{0xFFFF, 0x04, false},
	}
	for i, cycle := range want {
		driveBus.Address = 0xDEAD
		c.tick()
		if got := c.bus(); got != cycle {
			t.Fatalf("IRQ cycle %d: got %+v, want %+v", i, got, cycle)
		}
		if masked := *c.p&P_INTERRUPT != 0; masked != (i >= 5) {
			t.Fatalf("IRQ cycle %d: I set=%v, want %v", i, masked, i >= 5)
		}
	}
	if *c.pc != 0x0400 || *c.sp != 0xFC || *c.p&P_INTERRUPT == 0 {
		t.Fatalf("after entry: PC=$%04X SP=$%02X P=$%02X", *c.pc, *c.sp, *c.p)
	}
	c.tick()
	if *c.interrupt != 0 || *c.opcode != 0xEA {
		t.Fatal("first handler instruction was not fetched")
	}
}

func TestDriveIRQPersistentLevel(t *testing.T) {
	for source := range 2 {
		t.Run(fmt.Sprintf("source-%d", source), func(t *testing.T) {
			c := newDriveIRQTestCPU(t)
			*c.p = P_INTERRUPT
			c.program(0x0200, 0xEA, 0x58, 0xEA)
			c.program(0x0400, 0x40) // RTI without acknowledging the source.
			c.pin(source == 0, source == 1)
			c.cycles(2)
			c.checkEntry(t, false, 0) // CLI must execute despite held IRQ.
			c.tick()
			c.checkEntry(t, false, 0) // Instruction following CLI.
			c.tick()
			c.checkEntry(t, true, 0x0203)
			c.cycles(6) // Remaining entry cycles.
			c.cycles(6) // RTI restores I before its poll.
			c.checkEntry(t, true, 0x0203)
		})
	}
}

func TestDriveIRQLateLevel(t *testing.T) {
	c := newDriveIRQTestCPU(t)
	c.tick()
	c.pin(true, false) // Too late for this NOP's poll.
	c.tick()
	c.checkEntry(t, false, 0)
	c.tick()
	c.checkEntry(t, true, 0x0202)
}

func TestDriveIRQMaskedPulseIsNotQueued(t *testing.T) {
	c := newDriveIRQTestCPU(t)
	*c.p = P_INTERRUPT
	c.program(0x0200, 0xEA, 0x58, 0xEA, 0xEA)
	c.pin(true, false)
	c.cycles(2)
	c.pin(false, false)
	for range 3 {
		c.checkEntry(t, false, 0)
		c.tick()
	}
}

func TestDriveIRQOverlappingSources(t *testing.T) {
	c := newDriveIRQTestCPU(t)
	c.program(0x0200, 0xAD, 0x00, 0x03)
	c.pin(true, false)
	c.tick()
	c.pin(true, true)
	c.tick()
	c.pin(false, true)
	c.tick() // Sample the still-asserted second source.
	c.pin(false, false)
	c.tick()
	c.checkEntry(t, true, 0x0203)
}

func TestDriveIRQResetClearsAcceptance(t *testing.T) {
	c := newDriveIRQTestCPU(t)
	c.pin(true, false)
	c.cycles(2)
	c.pin(false, false)
	c.reset()
	if *c.p&P_INTERRUPT == 0 {
		t.Fatal("reset did not mask IRQ")
	}
	c.program(0x0200, 0x58, 0xEA, 0xEA)
	for range 3 {
		c.checkEntry(t, false, 0)
		c.tick()
	}
}

func TestDriveIRQInBranchLoop(t *testing.T) {
	c := newDriveIRQTestCPU(t)
	c.program(0x0200, 0xD0, 0xFE)
	c.cycles(2)
	c.pin(true, false) // After the first branch's only poll.
	c.tick()
	c.checkEntry(t, false, 0)
	c.cycles(2)
	c.checkEntry(t, true, 0x0200)
}

func TestDriveIRQPeripheralSampling(t *testing.T) {
	t.Run("timer", func(t *testing.T) {
		c := newDriveIRQTestCPU(t)
		// The two cores phase their timer against the CPU
		// differently, so they recognize an underflow a cycle apart.
		//
		// On the C64 the CIAs are clocked after the CPU within a bus
		// cycle (see CIA.Tick), which is the order the chips see Phi2
		// fall in: the 6510 latches its IRQ input on that edge and
		// the CIA's output only settles after it, so an underflow on
		// cycle N is first sampled on cycle N+1.
		//
		// That one cycle costs two here, because acceptance needs the
		// sample to be live at an instruction's poll cycle and NOP
		// only polls every second cycle. Slipping past one poll waits
		// for the next. The extra NOP retired is why entry lands a
		// byte further on.
		//
		// The 1541 has no VIC-II to hang a clock tree off, so its
		// VIAs are still clocked at the top of DriveCPU.TickPhi2 and
		// an underflow is sampled in the cycle it happens.
		via2 = VIA{t1c: 1, t1l: 0xFFFF, acr: 0x40, ier: 0x40}
		cycles, entry := 2, uint16(0x0201)
		c.cycles(cycles)
		c.checkEntry(t, true, entry)
	})
	t.Run("final-read-acknowledgement", func(t *testing.T) {
		c := newDriveIRQTestCPU(t)
		via1.ier, via1.ifr = viaIFRCA1, viaIFRCA1
		via1.updateIRQ()
		c.program(0x0200, 0xAD, 0x01, 0x18)
		c.cycles(4)
		if via1.IRQ {
			t.Fatal("final read did not acknowledge IRQ")
		}
		c.checkEntry(t, true, 0x0203)
	})
	t.Run("penultimate-write-acknowledgement", func(t *testing.T) {
		c := newDriveIRQTestCPU(t)
		via2.ier, via2.ifr = viaIFRCA1, viaIFRCA1
		via2.updateIRQ()
		c.program(0x0200, 0xEE, 0x0D, 0x1C)
		c.cycles(6)
		if vic.IRQ {
			t.Fatal("dummy write did not acknowledge IRQ")
		}
		c.checkEntry(t, false, 0)
	})
}

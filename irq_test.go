package tiny64

import (
	"fmt"
	"testing"

	"github.com/davecheney/tiny64/rom"
)

// The schedules below use bus-cycle numbering, not Visual6502's T-state
// names. See https://www.nesdev.org/wiki/CPU_interrupts and
// https://www.nesdev.org/wiki/Visual6502wiki/6502_Interrupt_Recognition_Stages_and_Tolerances.
type irqTestCPU struct {
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

func newIRQTestCPU(t *testing.T, core string) irqTestCPU {
	t.Helper()
	saveMachine(t)
	savedBus, savedDriveBus := bus, driveBus
	t.Cleanup(func() { bus, driveBus = savedBus, savedDriveBus })
	cpu = CPU{PC: 0x0200, SP: 0xFF, PortDDR: 0xFF}
	cia1, cia2 = CIA{}, CIA{}
	via1, via2 = VIA{}, VIA{}
	keyboard = Keyboard{}
	vic = VICII{BA: true, AEC: true}
	cartridge = Cartridge{}
	bus = Bus{}
	ram = [65536]byte{}

	var c irqTestCPU
	switch core {
	case "6510":
		c = irqTestCPU{
			pc: &cpu.PC, sp: &cpu.SP, p: &cpu.regP, x: &cpu.X, y: &cpu.Y,
			opcode: &cpu.Opcode, tstate: &cpu.TState, interrupt: &cpu.Interrupt,
			tick: func() {
				// One whole bus cycle, not just the CPU's share of it.
				// The C64's CIAs hang off the VIC-II's Phi2 rather than
				// off the CPU (see ciaTick), so a harness that called
				// only TickPhi2 would leave their timers frozen and the
				// peripheral-sourced subtests below would assert nothing.
				cpu.TickPhi2()
				ciaTick()
			},
			reset: cpu.Reset,
			pin: func(a, b bool) {
				vic.setIRQ(a)
				cia1.setIRQ(b)
			},
			put:  func(addr uint16, b byte) { ram[addr] = b },
			read: func(addr uint16) byte { return ram[addr] },
			bus:  func() busCycle { return busCycle{bus.Address, bus.Data, !bus.RW} },
		}
	case "6502":
		savedROM := rom.Drive1541
		rom.Drive1541 = make([]byte, len(savedROM))
		t.Cleanup(func() { rom.Drive1541 = savedROM })
		driveCPU = DriveCPU{PC: 0x0200, SP: 0xFF}
		driveRAM = [0x0800]byte{}
		driveBus = DriveBus{}
		via1, via2 = VIA{}, VIA{}
		via1SampleATN()
		c = irqTestCPU{
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
	default:
		t.Fatalf("unknown IRQ test core %q", core)
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

func (c irqTestCPU) program(addr uint16, code ...byte) {
	for i, b := range code {
		c.put(addr+uint16(i), b)
	}
}

func (c irqTestCPU) cycles(n int) {
	for range n {
		c.tick()
	}
}

func (c irqTestCPU) checkEntry(t *testing.T, want bool, pc uint16) {
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

func TestIRQInactiveCycleSampling(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		for _, pending := range []bool{false, true} {
			for sources := range 4 {
				for _, masked := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/pending=%t/sources=%d/masked=%t", core, pending, sources, masked), func(t *testing.T) {
						c := newIRQTestCPU(t, core)
						state := &cpu.irq
						if core == "6502" {
							state = &driveCPU.irq
						}
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
}

func TestIRQInstructionPulses(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		for _, tc := range []struct {
			name   string
			code   []byte
			cycles int
			pc     uint16
			setup  func(irqTestCPU)
		}{
			{"NOP", []byte{0xEA}, 2, 0x0201, nil},
			{"LDA-immediate", []byte{0xA9, 0x42}, 2, 0x0202, nil},
			{"LDA-zp", []byte{0xA5, 0x10}, 3, 0x0202, nil},
			{"LDA-zp-X", []byte{0xB5, 0x10}, 4, 0x0202, nil},
			{"LDA-absolute", []byte{0xAD, 0x00, 0x03}, 4, 0x0203, nil},
			{"LDA-absolute-X", []byte{0xBD, 0x00, 0x03}, 4, 0x0203, nil},
			{"LDA-absolute-X-cross", []byte{0xBD, 0xFF, 0x03}, 5, 0x0203, func(c irqTestCPU) { *c.x = 1 }},
			{"LDA-indirect-X", []byte{0xA1, 0x10}, 6, 0x0202, func(c irqTestCPU) { c.put(0x11, 0x03) }},
			{"LDA-indirect-Y", []byte{0xB1, 0x10}, 5, 0x0202, func(c irqTestCPU) { c.put(0x11, 0x03) }},
			{"LDA-indirect-Y-cross", []byte{0xB1, 0x10}, 6, 0x0202, func(c irqTestCPU) {
				*c.y = 1
				c.put(0x10, 0xFF)
				c.put(0x11, 0x03)
			}},
			{"STA-absolute", []byte{0x8D, 0x00, 0x03}, 4, 0x0203, nil},
			{"STA-indirect-X", []byte{0x81, 0x10}, 6, 0x0202, func(c irqTestCPU) { c.put(0x11, 0x03) }},
			{"INC-zp", []byte{0xE6, 0x10}, 5, 0x0202, nil},
			{"INC-absolute-X", []byte{0xFE, 0xFF, 0x03}, 7, 0x0203, func(c irqTestCPU) { *c.x = 1 }},
			{"PHA", []byte{0x48}, 3, 0x0201, nil},
			{"PLA", []byte{0x68}, 4, 0x0201, nil},
			{"JMP", []byte{0x4C, 0x00, 0x05}, 3, 0x0500, nil},
			{"JMP-indirect", []byte{0x6C, 0x10, 0x00}, 5, 0x0500, func(c irqTestCPU) { c.put(0x11, 0x05) }},
			{"JSR", []byte{0x20, 0x00, 0x05}, 6, 0x0500, nil},
			{"RTS", []byte{0x60}, 6, 0x0500, func(c irqTestCPU) {
				*c.sp = 0xFD
				c.put(0x01FE, 0xFF)
				c.put(0x01FF, 0x04)
			}},
		} {
			for pulse := range tc.cycles {
				t.Run(fmt.Sprintf("%s/%s/pulse-%d", core, tc.name, pulse), func(t *testing.T) {
					c := newIRQTestCPU(t, core)
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
}

func TestIRQBranchPulses(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
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
					t.Run(fmt.Sprintf("%s/%02X/%s/pulse-%d", core, branch.op, tc.name, pulse), func(t *testing.T) {
						c := newIRQTestCPU(t, core)
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
}

func TestIRQMaskPolling(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
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
			t.Run(core+"/"+tc.name, func(t *testing.T) {
				c := newIRQTestCPU(t, core)
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
}

func TestIRQEntryBusCycles(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		t.Run(core, func(t *testing.T) {
			c := newIRQTestCPU(t, core)
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
				bus.Address, driveBus.Address = 0xDEAD, 0xDEAD
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
		})
	}
}

func TestIRQPersistentLevel(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		for source := range 2 {
			t.Run(fmt.Sprintf("%s/source-%d", core, source), func(t *testing.T) {
				c := newIRQTestCPU(t, core)
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
}

func TestIRQLateLevel(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		t.Run(core, func(t *testing.T) {
			c := newIRQTestCPU(t, core)
			c.tick()
			c.pin(true, false) // Too late for this NOP's poll.
			c.tick()
			c.checkEntry(t, false, 0)
			c.tick()
			c.checkEntry(t, true, 0x0202)
		})
	}
}

func TestIRQMaskedPulseIsNotQueued(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		t.Run(core, func(t *testing.T) {
			c := newIRQTestCPU(t, core)
			*c.p = P_INTERRUPT
			c.program(0x0200, 0xEA, 0x58, 0xEA, 0xEA)
			c.pin(true, false)
			c.cycles(2)
			c.pin(false, false)
			for range 3 {
				c.checkEntry(t, false, 0)
				c.tick()
			}
		})
	}
}

func TestIRQOverlappingSources(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		t.Run(core, func(t *testing.T) {
			c := newIRQTestCPU(t, core)
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
		})
	}
}

func TestIRQResetClearsAcceptance(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		t.Run(core, func(t *testing.T) {
			c := newIRQTestCPU(t, core)
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
		})
	}
}

func TestIRQAcceptedAlongsideNMI(t *testing.T) {
	c := newIRQTestCPU(t, "6510")
	c.program(0x0200, 0xAD, 0x00, 0x03)
	c.program(0x0500, 0x40)
	c.put(0xFFFA, 0x00)
	c.put(0xFFFB, 0x05)
	cia2.setIRQ(true)
	c.cycles(2)
	c.pin(true, false)
	c.tick()
	c.pin(false, false)
	c.tick()
	c.tick()
	if cpu.Interrupt != 2 {
		t.Fatalf("simultaneous acceptance chose interrupt %d, want NMI", cpu.Interrupt)
	}
	c.cycles(6)
	c.cycles(6) // RTI restores I=0.
	c.checkEntry(t, false, 0)
	c.tick()
	c.checkEntry(t, false, 0)
}

func TestIRQInBranchLoop(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		t.Run(core, func(t *testing.T) {
			c := newIRQTestCPU(t, core)
			c.program(0x0200, 0xD0, 0xFE)
			c.cycles(2)
			c.pin(true, false) // After the first branch's only poll.
			c.tick()
			c.checkEntry(t, false, 0)
			c.cycles(2)
			c.checkEntry(t, true, 0x0200)
		})
	}
}

func TestIRQImplementedInstructionPaths(t *testing.T) {
	for _, tc := range instrTests {
		op := tc.ram[tc.initial.PC]
		switch op {
		case 0x00, 0x28, 0x40, 0x58, 0x78:
			continue // Entry and I-changing instructions have separate cases.
		}
		if op&0x1F == 0x10 {
			continue // Branches have their own poll schedule.
		}
		t.Run(tc.name, func(t *testing.T) {
			c := newIRQTestCPU(t, "6510")
			cpu.PC, cpu.SP = tc.initial.PC, tc.initial.SP
			cpu.A, cpu.X, cpu.Y = tc.initial.A, tc.initial.X, tc.initial.Y
			cpu.regP = tc.initial.Status &^ P_INTERRUPT
			for addr, b := range tc.ram {
				c.put(addr, b)
			}
			for cycle := range tc.cycles {
				c.pin(cycle == len(tc.cycles)-2, false)
				c.tick()
			}
			c.pin(false, false)
			c.checkEntry(t, true, tc.final.PC)
		})
	}
}

func TestIRQUndocumentedInstructions(t *testing.T) {
	for _, tc := range []struct {
		op     byte
		cycles int
	}{
		{0x0B, 2}, {0x2B, 2}, {0x1A, 2}, {0x80, 2},
		{0x04, 3}, {0x14, 4}, {0x0C, 4}, {0x1C, 4},
		{0xA7, 3}, {0xAF, 4}, {0xA3, 6}, {0xB3, 5},
		{0xB7, 4}, {0xBF, 4}, {0xC7, 5},
	} {
		t.Run(fmt.Sprintf("%02X", tc.op), func(t *testing.T) {
			c := newIRQTestCPU(t, "6510")
			c.program(0x0200, tc.op, 0x10, 0x03)
			c.put(0x11, 0x03)
			for cycle := range tc.cycles {
				c.pin(cycle == tc.cycles-2, false)
				c.tick()
			}
			c.pin(false, false)
			pc := *c.pc
			if *c.tstate != 0 {
				t.Fatalf("opcode $%02X did not finish in %d cycles", tc.op, tc.cycles)
			}
			c.checkEntry(t, true, pc)
		})
	}
}

// TestIRQMaskWriteUnmasksLatchedFlag covers the CIA interrupt check's
// third call site. Neither timer underflows here, so nothing inside Tick
// can notice that the flag became eligible: only the mask write itself
// can. Entry must still land on the instruction after the write, because
// the write cycle's own poll samples the pin before the store takes
// effect.
func TestIRQMaskWriteUnmasksLatchedFlag(t *testing.T) {
	c := newIRQTestCPU(t, "6510")
	cpu.Port = 6          // Expose I/O so the store reaches CIA1.
	cpu.A = 0x81          // Set (not clear) mask bit 0, Timer A.
	cia1 = CIA{icr: 0x01} // Timer A fired earlier while masked off.
	c.program(0x0200, 0x8D, 0x0D, 0xDC, 0xEA)

	c.cycles(4) // STA $DC0D: the write lands on the fourth cycle.
	if !cia1.IRQ {
		t.Fatal("unmasking an already latched flag did not assert IRQ")
	}
	if cia1.icr&0x80 == 0 {
		t.Fatalf("icr = %#02x after unmasking, want bit 7 set", cia1.icr)
	}
	c.cycles(2)
	c.checkEntry(t, true, 0x0204)
}

func TestIRQPeripheralSampling(t *testing.T) {
	for _, core := range []string{"6510", "6502"} {
		t.Run(core+"/timer", func(t *testing.T) {
			c := newIRQTestCPU(t, core)
			// The two cores phase their timer against the CPU
			// differently, so they recognize an underflow a cycle apart.
			//
			// On the C64 the CIAs are clocked after the CPU within a bus
			// cycle (see ciaTick), which is the order the chips see Phi2
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
			cycles, entry := 2, uint16(0x0201)
			if core == "6510" {
				cia1 = CIA{timerA: 1, latchA: 0xFFFF, runningA: true, imr: 1}
				cycles, entry = 4, 0x0202
			} else {
				via2 = VIA{t1c: 1, t1l: 0xFFFF, acr: 0x40, ier: 0x40}
			}
			c.cycles(cycles)
			c.checkEntry(t, true, entry)
		})
		t.Run(core+"/final-read-acknowledgement", func(t *testing.T) {
			c := newIRQTestCPU(t, core)
			if core == "6510" {
				cpu.Port = 6 // Expose I/O; the test stops before reading ROM vectors.
				cia1 = CIA{icr: 0x81, imr: 1, IRQ: true}
				cia1.setIRQ(true)
				c.program(0x0200, 0xAD, 0x0D, 0xDC)
			} else {
				via1.ier, via1.ifr = viaIFRCA1, viaIFRCA1
				via1.updateIRQ()
				c.program(0x0200, 0xAD, 0x01, 0x18)
			}
			c.cycles(4)
			if cia1.IRQ || via1.IRQ {
				t.Fatal("final read did not acknowledge IRQ")
			}
			c.checkEntry(t, true, 0x0203)
		})
		t.Run(core+"/penultimate-write-acknowledgement", func(t *testing.T) {
			c := newIRQTestCPU(t, core)
			if core == "6510" {
				cpu.Port = 6
				vic.interruptEnable, vic.interruptStatus = 1, 1
				vic.updateIRQ()
				c.program(0x0200, 0xEE, 0x19, 0xD0)
			} else {
				via2.ier, via2.ifr = viaIFRCA1, viaIFRCA1
				via2.updateIRQ()
				c.program(0x0200, 0xEE, 0x0D, 0x1C)
			}
			c.cycles(6)
			if vic.IRQ || via2.IRQ {
				t.Fatal("dummy write did not acknowledge IRQ")
			}
			c.checkEntry(t, false, 0)
		})
	}
}

// RDY holds the instruction sequencer, but not the synchronizer or the
// clock-T0/branch-T2 acceptance circuitry. These pulse windows are from
// transistor-model schedules, not from irqState's deferred-poll reduction.
// Cycle numbers include three extra physical cycles at the held read.
func TestIRQReadStalls(t *testing.T) {
	for _, tc := range []struct {
		name       string
		code       []byte
		pc, target uint16
		cycles     int
		hold       int
		beforeI    byte
		stackI     byte
		accepted   []int
	}{
		{"NOP-fetch", []byte{0xEA}, 0x0200, 0x0201, 2, 0, 0, 0, []int{3}},
		{"NOP-terminal", []byte{0xEA}, 0x0200, 0x0201, 2, 1, 0, 0, []int{0, 1, 2, 3}},
		{"LDA-operand", []byte{0xAD, 0x00, 0x03}, 0x0200, 0x0203, 4, 1, 0, 0, []int{5}},
		{"LDA-terminal", []byte{0xAD, 0x00, 0x03}, 0x0200, 0x0203, 4, 3, 0, 0, []int{2, 3, 4, 5}},
		{"CLI-terminal", []byte{0x58}, 0x0200, 0x0201, 2, 1, P_INTERRUPT, 0, []int{1, 2, 3}},
		{"SEI-terminal", []byte{0x78}, 0x0200, 0x0201, 2, 1, 0, 0, []int{0}},
		{"PLP-clear", []byte{0x28}, 0x0200, 0x0201, 4, 3, P_INTERRUPT, 0, nil},
		{"PLP-set", []byte{0x28}, 0x0200, 0x0201, 4, 3, 0, P_INTERRUPT, []int{2, 3, 4, 5}},
		{"RTI-status", []byte{0x40}, 0x0200, 0x0500, 6, 3, P_INTERRUPT, 0, []int{7}},
		{"RTI-terminal", []byte{0x40}, 0x0200, 0x0500, 6, 5, P_INTERRUPT, 0, []int{4, 5, 6, 7}},
		{"branch-not-taken-operand", []byte{0xF0, 0x02}, 0x0200, 0x0202, 2, 1, 0, 0, []int{0, 1, 2, 3}},
		{"branch-taken-operand", []byte{0xD0, 0x02}, 0x0200, 0x0204, 3, 1, 0, 0, []int{0, 1, 2, 3}},
		{"branch-same-page-terminal", []byte{0xD0, 0x02}, 0x0200, 0x0204, 3, 2, 0, 0, []int{0}},
		{"branch-crossing-operand", []byte{0xD0, 0xFC}, 0x0300, 0x02FE, 4, 1, 0, 0, []int{0, 1, 2, 3, 5}},
		{"branch-crossing-terminal", []byte{0xD0, 0xFC}, 0x0300, 0x02FE, 4, 3, 0, 0, []int{0, 2, 3, 4, 5}},
	} {
		for pulse := range tc.cycles + 3 {
			t.Run(fmt.Sprintf("%s/pulse-%d", tc.name, pulse), func(t *testing.T) {
				c := newIRQTestCPU(t, "6510")
				cpu.PC, cpu.SP, cpu.regP = tc.pc, 0xFC, tc.beforeI
				c.program(tc.pc, tc.code...)
				c.program(0x01FD, tc.stackI, 0x00, 0x05)
				for cycle := range tc.cycles + 3 {
					stalled := cycle >= tc.hold && cycle < tc.hold+3
					vic.AEC = !stalled
					c.pin(cycle == pulse, false)
					pc, state := cpu.PC, cpu.TState
					c.tick()
					if stalled && (cpu.PC != pc || cpu.TState != state) {
						t.Fatalf("cycle %d advanced a held read", cycle)
					}
					if tc.code[0] == 0x58 && cycle >= 1 && cpu.regP&P_INTERRUPT != 0 {
						t.Fatalf("cycle %d: CLI failed to clear I while held", cycle)
					}
					if tc.code[0] == 0x78 && cycle >= 1 && cpu.regP&P_INTERRUPT == 0 {
						t.Fatalf("cycle %d: SEI failed to set I while held", cycle)
					}
					if tc.code[0] == 0x28 && stalled && cpu.regP&P_INTERRUPT != tc.beforeI {
						t.Fatalf("cycle %d: PLP changed I before its completing read", cycle)
					}
				}
				vic.AEC = true
				c.pin(false, false)
				if cpu.TState != 0 || cpu.PC != tc.target {
					t.Fatalf("after instruction: PC=$%04X TState=%d", cpu.PC, cpu.TState)
				}
				want := false
				for _, accepted := range tc.accepted {
					want = want || pulse == accepted
				}
				c.checkEntry(t, want, tc.target)
			})
		}
	}
}

func TestIRQEntryReadStallsAndWrites(t *testing.T) {
	for state := uint8(0); state < 7; state++ {
		t.Run(fmt.Sprintf("TState-%d", state), func(t *testing.T) {
			c := newIRQTestCPU(t, "6510")
			c.pin(true, false)
			c.cycles(2)
			c.pin(false, false)
			c.cycles(int(state))
			pc, sp := cpu.PC, cpu.SP
			vic.AEC = false
			if state >= 2 && state <= 4 {
				c.tick()
				if cpu.TState != state+1 || cpu.SP != sp-1 {
					t.Fatal("RDY stopped an interrupt stack write")
				}
			} else {
				c.cycles(3)
				if cpu.TState != state || cpu.PC != pc || cpu.SP != sp {
					t.Fatal("RDY did not hold an interrupt read")
				}
				vic.AEC = true
				c.tick()
			}
			vic.AEC = true
			c.cycles(6 - int(state))
			if cpu.PC != 0x0400 || cpu.SP != 0xFC {
				t.Fatalf("entry corrupted by hold: PC=$%04X SP=$%02X", cpu.PC, cpu.SP)
			}
		})
	}
}

func TestIRQHeldPollNMIArbitration(t *testing.T) {
	c := newIRQTestCPU(t, "6510")
	c.program(0x0500, 0x40)
	c.put(0xFFFA, 0x00)
	c.put(0xFFFB, 0x05)
	c.pin(true, false)
	c.tick()
	vic.AEC = false
	c.pin(false, false)
	c.tick() // IRQ qualifies in the held NOP poll.
	cia2.setIRQ(true)
	c.cycles(3)
	vic.AEC = true
	c.tick()
	c.tick()
	if cpu.Interrupt != 2 {
		t.Fatalf("NMI lost priority after a held IRQ poll: interrupt=%d", cpu.Interrupt)
	}
	c.cycles(12) // Finish entry and execute RTI.
	c.checkEntry(t, false, 0)
}

func TestIRQResetDuringHeldPoll(t *testing.T) {
	c := newIRQTestCPU(t, "6510")
	c.pin(true, false)
	c.tick()
	vic.AEC = false
	c.pin(false, false)
	c.cycles(2)
	c.reset()
	vic.AEC = true
	c.program(0x0200, 0x58, 0xEA, 0xEA)
	for range 3 {
		c.checkEntry(t, false, 0)
		c.tick()
	}
}

func TestIRQEntryDoesNotPoll(t *testing.T) {
	for state := uint8(0); state <= 6; state++ {
		if irqPoll(0x00, state, 0) {
			t.Fatalf("BRK/IRQ/NMI entry polled at TState %d", state)
		}
	}
	for opcode := range 256 {
		if irqPoll(uint8(opcode), 0, 1) {
			t.Fatalf("opcode fetch polled with preceding opcode $%02X", opcode)
		}
	}
}

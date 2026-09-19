package tiny64

import (
	"fmt"
	"os"
	"testing"
)

// TestMain checks that the suite leaves the IEC bus as it found it.
//
// A peripheral left attached is not passive: iecTick clocks everything on
// the bus on every Phi2, so a stray drive goes on driving CLK and DATA
// into whatever test runs next. That makes it a source of order-dependent
// failures - which is how it was found, as a drive that outlived its test
// and broke a 1541 test that had been passing only because it happened to
// sort first.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 && len(iecBus) != 0 {
		fmt.Fprintf(os.Stderr, "tests leaked %d IEC peripheral(s):\n", len(iecBus))
		for _, p := range iecBus {
			fmt.Fprintf(os.Stderr, "\t%T at address %d\n", p, p.iecAddress())
		}
		fmt.Fprintln(os.Stderr, "every test that attaches a drive must put the bus back; see saveMachine")
		code = 1
	}
	os.Exit(code)
}

// stubPeripheral is an inert device that does nothing but answer to an
// address. The bus only ever asks a peripheral for its three lines and its
// address, so that is all a test of the bus needs.
//
// It exists because the properties under test - that two devices at one
// address collapse to one, and that two at different addresses both
// survive - cannot be exercised with a single type, drive1541 being
// permanently at address 8.
type stubPeripheral struct{ addr uint8 }

func (*stubPeripheral) iecCLKOut() bool     { return false }
func (*stubPeripheral) iecDATAOut() bool    { return false }
func (*stubPeripheral) iecTick()            {}
func (s *stubPeripheral) iecAddress() uint8 { return s.addr }

// saveBus isolates a test from the package-level bus.
func saveBus(t *testing.T) {
	saved := append([]iecPeripheral(nil), iecBus...)
	t.Cleanup(func() { iecBus = saved })
	iecBus = nil
}

// Two devices answering to one address is the failure this guards against.
// It does not look like a collision from the C64's side: both devices hear
// the LISTEN, both reply, and the contention on the open-collector lines
// comes back as a corrupt byte - a directory load that ends in ?FILE NOT
// FOUND, which reads as a missing file rather than as a bus fault.
//
// attachIEC used to dedupe by concrete type. Type answers "you cannot plug
// the same drive in twice"; it does not answer "two devices are answering
// address 8", which is the only question a bus cares about. So this uses
// two deliberately *different* types at one address: under the old rule
// they were distinct, and both stayed.
func TestAttachIECReplacesByAddress(t *testing.T) {
	saveBus(t)

	AttachVirtualDrive(8) // a drive at 8
	replacement := &stubPeripheral{addr: 8}
	attachIEC(replacement) // different type, same address

	if len(iecBus) != 1 {
		t.Fatalf("two devices at address 8: bus has %d peripherals, want 1", len(iecBus))
	}
	if iecBus[0] != iecPeripheral(replacement) {
		t.Fatalf("bus holds %T, want the device attached last", iecBus[0])
	}
}

// Different addresses are not a collision, and both must stay. Without
// this, keying on address could collapse into "one device, ever" and still
// satisfy the test above.
func TestAttachIECKeepsDistinctAddresses(t *testing.T) {
	saveBus(t)

	AttachVirtualDrive(8)               // a drive at 8
	attachIEC(&stubPeripheral{addr: 9}) // second device at 9

	if len(iecBus) != 2 {
		t.Fatalf("bus has %d peripherals, want 2 (a drive at 8 and one at 9)", len(iecBus))
	}
}

// detachIEC used to filter into iecBus[:0], which writes through the
// backing array. Anything holding a snapshot of the bus - saveMachine, or
// any test restoring state - would then find its copy had quietly acquired
// whichever device was attached next. The length stayed right, so it never
// looked like corruption; it surfaced as a drive still being clocked long
// after the test that detached it had finished.
func TestDetachIECDoesNotWriteThroughSnapshots(t *testing.T) {
	saveBus(t)

	AttachVirtualDrive(8)
	snapshot := iecBus

	DetachVirtualDrive()                // detach, freeing slot 0 of the shared array
	attachIEC(&stubPeripheral{addr: 8}) // attach, which used to reuse that slot

	if _, ok := snapshot[0].(*iecDevice); !ok {
		t.Fatalf("snapshot of the bus now holds %T, want the drive it was taken of", snapshot[0])
	}
}

// A detached device must stop being clocked. iecTick runs on every Phi2,
// so a peripheral left on the bus is not inert - it keeps driving lines.
func TestDetachIECStopsTicking(t *testing.T) {
	saveBus(t)

	dev := &stubPeripheral{addr: 8}
	attachIEC(dev)
	detachIEC(dev)

	if len(iecBus) != 0 {
		t.Fatalf("bus has %d peripherals after detach, want 0", len(iecBus))
	}
}

// The replace path has the same hazard as the append and detach paths, and
// is reachable without detaching anything: plugging a device in at an
// address someone else already occupies used to assign straight into the
// backing array, so any reference to the bus saw its contents change under
// it.
//
// This drives it through AttachVirtualDrive rather than calling attachIEC
// directly, because that is the production route: AttachVirtualDrive
// attaches straight onto whatever address 8 already holds without
// detaching first. A version of this test that displaces the occupant with
// a call that detaches first passes with the bug present - it never
// reaches the replace path at all, and looks identical to one that does.
func TestAttachIECDoesNotWriteThroughSnapshots(t *testing.T) {
	saveBus(t)

	attachIEC(&stubPeripheral{addr: 8}) // occupy address 8
	snapshot := iecBus

	AttachVirtualDrive(8) // displaces it in place, no detach

	if _, ok := snapshot[0].(*stubPeripheral); !ok {
		t.Fatalf("snapshot of the bus now holds %T, want the device it was taken of", snapshot[0])
	}
}

// countingPeripheral records how many times the bus clocked it, and
// checks that each clock arrived one bus cycle after the last, behind the
// CPU.
//
// The count alone only says the bus was clocked often enough; it does not
// say it was clocked at the right moments. A path that clocked every
// device twice on half the cycles would have the same total as one that
// clocked it once on each.
//
// CIA2's timer A tells those apart. armPhi2Counter leaves it free-running
// on Phi2, and stepCycle calls ciaTick once per bus cycle, after the CPU
// and ahead of the IEC devices - which is the clock tree this test exists
// to pin. So at the head of the nth iecTick the timer must have counted
// down exactly n+1 times: one more and some cycle clocked this device
// twice, one fewer and some cycle skipped it.
//
// Nothing here reads the beam. A device on the serial bus cannot observe
// the VIC, and where Phi2 falls inside the VIC's eight dots is the VIC's
// own business - TestVICStepCycleCPUWriteLandsMidSlot pins that, through
// the pixels it moves, which is where it is actually observable.
type countingPeripheral struct {
	addr      uint8
	ticks     int
	startPhi2 uint16
	wrongPhi2 int
}

func (*countingPeripheral) iecCLKOut() bool  { return false }
func (*countingPeripheral) iecDATAOut() bool { return false }

func (c *countingPeripheral) iecTick() {
	if cia2.timerA != c.startPhi2-uint16(c.ticks)-1 {
		c.wrongPhi2++
	}
	c.ticks++
}

func (c *countingPeripheral) iecAddress() uint8 { return c.addr }

// armPhi2Counter turns CIA2's timer A into a free-running count of Phi2
// cycles and returns its starting value. CIA2 is the one the KERNAL leaves
// idle - CIA1 owns the jiffy interrupt - and 0xFFFF is more than three
// PAL frames of headroom, so it counts straight down without underflowing
// and reloading, and without ever raising the NMI it is wired to.
func armPhi2Counter() uint16 {
	cia2.Store(0x0E, 0x00) // stop, so the high byte write loads the counter
	cia2.Store(0x04, 0xFF)
	cia2.Store(0x05, 0xFF)
	cia2.Store(0x0E, 0x01) // start, continuous, counting Phi2
	return cia2.timerA
}

// Both public stepping APIs must clock IEC once per cycle, after the CPU,
// including on blanked lines and across frame wraps. These assertions cover
// clock cadence, not TinyGo-specific rendering or inlining: those require
// validation with that build's window and compiler.
func TestBusIsClockedOnEveryFramePath(t *testing.T) {
	for _, tc := range []struct {
		name        string
		startCycles int
		run         func()
	}{
		{"StepFrame", 0, StepFrame},
		{"StepCycle", 0, func() {
			for range CyclesPerFrame {
				vic.StepCycle()
			}
		}},
		// Starting on a line other than zero, which is what this case is
		// for, but on a line boundary: StepFrame requires one. Stepping
		// part-way into a line and then asking for a frame is what
		// StepCycle is for, and the case above covers it.
		{"Interleaved", 45 * CyclesPerLine, StepFrame},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newMachine(t)
			// One frame of warm-up first. IOINIT, in the first hundred
			// cycles after reset, is the only code in the boot path that
			// writes CIA2's timer registers, and timer A has to be left
			// alone to stand in for the cycle counter below.
			m.run(CyclesPerFrame)
			m.run(tc.startCycles)
			// Not an IEC property, but a whole frame has to have gone by
			// for the tick count below to mean what it says.
			startDot, startLine := vic.dot, vic.rasterLine
			dev := &countingPeripheral{
				addr:      9,
				startPhi2: armPhi2Counter(),
			}
			attachIEC(dev)
			tc.run()

			if vic.dot != startDot || vic.rasterLine != startLine {
				t.Errorf("frame ended at dot %d line %d, want dot %d line %d", vic.dot, vic.rasterLine, startDot, startLine)
			}
			if dev.ticks != CyclesPerFrame {
				t.Errorf("IEC ticks = %d, want %d", dev.ticks, CyclesPerFrame)
			}
			if !cia2.runningA || cia2.latchA != 0xFFFF {
				t.Fatal("the emulated program reprogrammed CIA2 timer A, which this test is using as its Phi2 count")
			}
			if got := dev.startPhi2 - cia2.timerA; got != CyclesPerFrame {
				t.Errorf("CPU Phi2 cycles = %d, want %d", got, CyclesPerFrame)
			}
			if dev.wrongPhi2 != 0 {
				t.Errorf("%d IEC ticks did not follow the corresponding CPU Phi2", dev.wrongPhi2)
			}
			vic.StepCycle()
			if dev.ticks != CyclesPerFrame+1 || dev.wrongPhi2 != 0 {
				t.Fatal("StepCycle after the frame did not preserve CPU/IEC cadence")
			}
		})
	}
}

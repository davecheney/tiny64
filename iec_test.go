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
	savedAttached := driveAttached
	t.Cleanup(func() {
		driveAttached = savedAttached
		iecBus = saved
	})
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

	AttachDrive(true) // 1541 at 8
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

	AttachDrive(true)                   // 1541 at 8
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

	AttachDrive(true)
	snapshot := iecBus

	AttachDrive(false)                  // detach, freeing slot 0 of the shared array
	attachIEC(&stubPeripheral{addr: 8}) // attach, which used to reuse that slot

	if _, ok := snapshot[0].(*drive1541); !ok {
		t.Fatalf("snapshot of the bus now holds %T, want the 1541 it was taken of", snapshot[0])
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
// This drives it through AttachDrive rather than calling attachIEC
// directly, because that is the production route: AttachDrive attaches
// straight onto whatever address 8 already holds without detaching first,
// and InsertDisk goes that way. A version of this test that displaces the
// occupant with a call that detaches first passes with the bug present -
// it never reaches the replace path at all, and looks identical to one
// that does.
func TestAttachIECDoesNotWriteThroughSnapshots(t *testing.T) {
	saveBus(t)

	attachIEC(&stubPeripheral{addr: 8}) // occupy address 8
	snapshot := iecBus

	AttachDrive(true) // displaces it in place, no detach

	if _, ok := snapshot[0].(*stubPeripheral); !ok {
		t.Fatalf("snapshot of the bus now holds %T, want the device it was taken of", snapshot[0])
	}
}

// countingPeripheral records how many times the bus clocked it.
type countingPeripheral struct {
	addr  uint8
	ticks int
}

func (*countingPeripheral) iecCLKOut() bool     { return false }
func (*countingPeripheral) iecDATAOut() bool    { return false }
func (c *countingPeripheral) iecTick()          { c.ticks++ }
func (c *countingPeripheral) iecAddress() uint8 { return c.addr }

// Every path that advances a frame has to clock the bus, and there is more
// than one: FinishFrame steps dot by dot through StepDot, while StepFrame
// takes the faster per-cycle route through stepCycle. They are separate
// call sites, so adding iecTick to one and not the other is an easy thing
// to do and a hard thing to notice.
//
// It is hard to notice because nothing else catches it. Removing iecTick
// from stepCycle leaves the entire rest of the suite green - the tests
// drive the machine through paths that still clock the bus - while the GUI
// front ends, which are the callers that use StepFrame, silently stop
// clocking every device attached. A drive would simply never respond, with
// no failure anywhere to say why.
func TestBusIsClockedOnEveryFramePath(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(v *VICII)
	}{
		{"StepFrame", func(v *VICII) { v.StepFrame() }},
		{"FinishFrame", func(v *VICII) { v.FinishFrame() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// newMachine, not saveBus alone: stepping the VIC means
			// running the CPU, and it will execute whatever a previous
			// test left in RAM unless the machine is reset first.
			newMachine(t)

			dev := &countingPeripheral{addr: 9}
			attachIEC(dev)
			tc.run(&vic)

			if dev.ticks == 0 {
				t.Fatalf("%s never clocked the bus: every attached device would stop running, and no other test would say so", tc.name)
			}
		})
	}
}

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
// survive - cannot be exercised with a single type, the virtual drive
// being permanently at whatever address it was attached to.
type stubPeripheral struct{ addr uint8 }

func (*stubPeripheral) iecCLKOut() bool     { return false }
func (*stubPeripheral) iecDATAOut() bool    { return false }
func (*stubPeripheral) iecTick()            {}
func (s *stubPeripheral) iecAddress() uint8 { return s.addr }

// saveBus isolates a test from the package-level bus.
func saveBus(t *testing.T) {
	saved := append([]iecPeripheral(nil), iecBus...)
	savedAttached := virtualDriveAttached
	t.Cleanup(func() {
		virtualDriveAttached = savedAttached
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

	AttachVirtualDrive(8) // virtual drive at 8
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

	AttachVirtualDrive(8)               // virtual drive at 8
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
		t.Fatalf("snapshot of the bus now holds %T, want the virtual drive it was taken of", snapshot[0])
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
// detaching first, and InsertDisk goes that way. A version of this test
// that displaces the occupant with a call that detaches first passes with
// the bug present - it never reaches the replace path at all, and looks
// identical to one that does.
func TestAttachIECDoesNotWriteThroughSnapshots(t *testing.T) {
	saveBus(t)

	attachIEC(&stubPeripheral{addr: 8}) // occupy address 8
	snapshot := iecBus

	AttachVirtualDrive(8) // displaces it in place, no detach

	if _, ok := snapshot[0].(*stubPeripheral); !ok {
		t.Fatalf("snapshot of the bus now holds %T, want the device it was taken of", snapshot[0])
	}
}

// countingPeripheral records how many times the bus clocked it, and where
// the beam stood each time.
//
// The count alone only says the bus was clocked often enough; it does not
// say it was clocked at the right moments. A path that clocked every
// device twice on half the cycles would have the same total as one that
// clocked it once on each. The fingerprint folds the beam position of
// every tick, in order, into a single value, so two paths agree on it only
// if they clocked the bus the same number of times, in the same order, at
// the same point in the frame.
type countingPeripheral struct {
	addr        uint8
	ticks       int
	fingerprint uint64
}

func (*countingPeripheral) iecCLKOut() bool  { return false }
func (*countingPeripheral) iecDATAOut() bool { return false }

func (c *countingPeripheral) iecTick() {
	c.ticks++
	// FNV-1a over the beam position, which is order sensitive: the same
	// set of positions visited in a different order hashes differently.
	for _, b := range []byte{
		byte(vic.dot), byte(vic.dot >> 8),
		byte(vic.rasterLine), byte(vic.rasterLine >> 8),
	} {
		c.fingerprint = (c.fingerprint ^ uint64(b)) * 1099511628211
	}
}

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
//
// Presence is not enough to pin that down, though, and this test used to
// check no more than that: a path that clocked the bus once per frame
// would have satisfied it. What the drive actually depends on is cadence
// and phase - one tick per bus cycle, taken at the same point in the cycle
// - because the 1541 counts bus clocks to time its own serial handshake.
// So each path is held to an exact CyclesPerFrame ticks, and the two are
// required to agree on a fingerprint of where the beam stood at every one
// of them. The fingerprints are compared against each other rather than
// against a constant: the property worth guarding is that the two front
// ends clock the bus identically, and a hardcoded hash would say nothing
// about that to whoever has to change it later.
//
// What comparing the two paths with each other cannot see is a divergence
// between build configurations. Both front ends compile the same way under
// gc, so a change that alters when the dotclocks run only under TinyGo
// would leave the two fingerprints agreeing with each other while the
// TinyGo build differed from both.
//
// That reaches less far than it sounds, though, and it is worth being
// exact about where the edge is, because believing this guard is blinder
// than it is invites someone to treat a real failure as out of scope.
// Work that must run every cycle escaping the every-cycle path is caught
// here, even when the escape is only reachable on another build. Should
// the dotclocks ever become conditional - skipped while the beam is
// outside the rendering window, say - and something on the bus or CPU
// path be moved inside them, the count assertion above fails under gc on
// the spot: a frame's ticks drop to the number of cycles that survived
// the condition, and the fingerprints are never reached. That was
// measured against such a change rather than assumed.
//
// What survives is narrower and differently shaped: a divergence that
// depends on the two builds' windows differing, rather than on anything
// being skipped. Work correct across one build's bounds but not the
// other's, or chip state gated at a crop boundary that only one build
// has, changes no tick count and moves no tick - so a frame looks
// identical from the bus. Closing that needs a second, build-tagged test
// taking the same fingerprint under the TinyGo constraints and comparing
// it with this one.
func TestBusIsClockedOnEveryFramePath(t *testing.T) {
	// clockedFrame runs one frame through the given path with a counting
	// device on the bus, and reports how that path clocked it.
	clockedFrame := func(t *testing.T, run func(v *VICII)) *countingPeripheral {
		t.Helper()
		// newMachine, not saveBus alone: stepping the VIC means
		// running the CPU, and it will execute whatever a previous
		// test left in RAM unless the machine is reset first. It also
		// restores the bus on cleanup, so nothing stays attached.
		newMachine(t)

		dev := &countingPeripheral{addr: 9, fingerprint: 14695981039346656037}
		attachIEC(dev)
		run(&vic)
		return dev
	}

	fingerprints := map[string]uint64{}
	frameOK := true
	for _, tc := range []struct {
		name string
		run  func(v *VICII)
	}{
		{"StepFrame", func(v *VICII) { v.StepFrame() }},
		{"FinishFrame", func(v *VICII) { v.FinishFrame() }},
	} {
		ok := t.Run(tc.name, func(t *testing.T) {
			dev := clockedFrame(t, tc.run)

			if dev.ticks == 0 {
				t.Fatalf("%s never clocked the bus: every attached device would stop running, and no other test would say so", tc.name)
			}
			if dev.ticks != CyclesPerFrame {
				t.Errorf("%s clocked the bus %d times in a frame, want CyclesPerFrame (%d): the bus runs at the CPU's Phi2, so a device sees exactly one tick per bus cycle", tc.name, dev.ticks, CyclesPerFrame)
			}
			fingerprints[tc.name] = dev.fingerprint
		})
		frameOK = frameOK && ok
	}

	if !frameOK {
		return // the fingerprints of a wrong frame say nothing useful
	}
	if a, b := fingerprints["StepFrame"], fingerprints["FinishFrame"]; a != b {
		t.Errorf("StepFrame clocked the bus at beam positions fingerprinting %#x, FinishFrame at %#x: the two paths agree on how many ticks a frame takes but not on when they happen, so a device timed off the bus behaves differently depending on which front end drives it", a, b)
	}
}

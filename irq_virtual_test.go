//go:build !drive1541

package tiny64

import "testing"

// irqTestCores is the set of CPU cores the IRQ suite runs against. The
// 6502 arm is the 1541's drive CPU, so a build without the physical drive
// has only the C64's 6510 to test.
var irqTestCores = []string{"6510"}

// saveIRQDriveState has nothing to save: there is no drive bus in this
// build.
func saveIRQDriveState(*testing.T) {}

// newIRQTestCPUForCore is reached only for a core name this build does not
// have. "6502" lands here rather than in irqTestCores, so a hand-written
// -run selecting it gets told why instead of silently passing.
func newIRQTestCPUForCore(t *testing.T, core string) irqTestCPU {
	t.Helper()
	if core == "6502" {
		t.Skip("the 6502 core is the 1541's; build with -tags drive1541 to test it")
	}
	t.Fatalf("unknown IRQ test core %q", core)
	return irqTestCPU{}
}

package tiny64

import "testing"

// requireBusTrace skips a test that asserts on Bus.Address/Bus.RW (or the
// DriveBus equivalents) when those lines are not being latched.
//
// Builds with the nobustrace tag - the stand-in the ordinary Go toolchain
// has for the TinyGo firmware configuration, see bus_notrace.go - drop those
// stores, so a cycle trace read back out of the bus would be stale rather
// than wrong-looking, and the test would fail for a reason that has nothing
// to do with the behaviour it is checking.
//
// Everything these tests cover still runs in the default configuration,
// which is what CI's plain `go test ./...` exercises; the nobustrace run is
// there to prove the emulator itself does not secretly depend on the
// diagnostic lines.
func requireBusTrace(t *testing.T) {
	t.Helper()
	if !BusTrace {
		t.Skip("bus Address/RW tracing is compiled out (nobustrace/tinygo build)")
	}
}

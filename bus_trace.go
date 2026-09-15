//go:build !tinygo && !nobustrace

package tiny64

// BusTrace reports whether Bus and DriveBus latch their Address and RW
// lines on every Phi2 access. It is a compile-time constant so that the
// stores guarded by it fold away entirely in builds that do not need them;
// see bus_notrace.go for why that matters and which builds those are.
const BusTrace = true

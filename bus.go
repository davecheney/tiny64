package tiny64

// Bus represents the physical wiring between components.
//
// Address and RW are not part of the machine's behaviour: nothing in the
// emulator reads them back, they exist so that tests and cmd/c64cli's
// tracer can see which cycle just ran. They are therefore only maintained
// when the BusTrace build constant is set - see bus_notrace.go.
type Bus struct {
	Address uint16
	Data    uint8
	RW      bool // true = Read, false = Write
}

// GetBus returns the singleton Bus instance, for debugging/tracing tools.
// Address and RW are only meaningful if BusTrace is true.
func GetBus() *Bus {
	return &bus
}

// Load performs a Phi2 read cycle at addr: it asserts the address and
// RW=true (read) on the bus, and returns the byte read through the PLA.
func (b *Bus) Load(addr uint16) uint8 {
	if BusTrace {
		b.Address = addr
		b.RW = true
	}
	b.Data = plaLoad(addr)
	return b.Data
}

// Store performs a Phi2 write cycle at addr: it asserts the address,
// RW=false (write), and the value on the bus, and writes it through the PLA.
func (b *Bus) Store(addr uint16, val uint8) {
	if BusTrace {
		b.Address = addr
		b.RW = false
	}
	b.Data = val
	plaStore(addr, val)
}

var bus Bus

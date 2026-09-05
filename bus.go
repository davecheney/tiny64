package tiny64

// Bus represents the physical wiring between components
type Bus struct {
	Address uint16
	Data    uint8
	RW      bool // true = Read, false = Write
}

// GetBus returns the singleton Bus instance, for debugging/tracing tools.
func GetBus() *Bus {
	return &bus
}

// Load performs a Phi2 read cycle at addr: it asserts the address and
// RW=true (read) on the bus, and returns the byte read through the PLA.
func (b *Bus) Load(addr uint16) uint8 {
	b.Address = addr
	b.RW = true
	b.Data = pla.Load(addr)
	return b.Data
}

// Store performs a Phi2 write cycle at addr: it asserts the address,
// RW=false (write), and the value on the bus, and writes it through the PLA.
func (b *Bus) Store(addr uint16, val uint8) {
	b.Address = addr
	b.RW = false
	b.Data = val
	pla.Store(addr, val)
}

var bus Bus

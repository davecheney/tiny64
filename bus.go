package tiny64

// Bus represents the physical wiring between components
type Bus struct {
	Address uint16
	Data    uint8
	RW      bool // true = Read, false = Write
}

// Load performs a Phi2 read cycle at addr: it asserts the address and
// RW=true (read) on the bus, and returns the byte at that location.
func (b *Bus) Load(addr uint16) uint8 {
	b.Address = addr
	b.RW = true
	b.Data = ram[addr]
	return b.Data
}

// Store performs a Phi2 write cycle at addr: it asserts the address,
// RW=false (write), and the value on the bus, and writes it to memory.
func (b *Bus) Store(addr uint16, val uint8) {
	b.Address = addr
	b.RW = false
	b.Data = val
	ram[addr] = val
}

var bus Bus

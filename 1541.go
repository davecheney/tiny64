package tiny64

import "github.com/davecheney/tiny64/rom"

// driveRAM is the 1541's 2K static RAM at $0000-$07FF.
var driveRAM [0x0800]byte

// DriveBus represents the physical wiring between the drive's CPU and its
// RAM/ROM/VIAs - the 1541 equivalent of Bus/PLA, but much simpler since the
// 1541 has no banking: the memory map is fixed.
type DriveBus struct {
	Address uint16
	Data    uint8
	RW      bool // true = Read, false = Write
}

var driveBus DriveBus

// GetDriveBus returns the singleton DriveBus instance, for debugging/
// tracing tools.
func GetDriveBus() *DriveBus {
	return &driveBus
}

// Load performs a Phi2 read cycle at addr on the drive's bus.
func (b *DriveBus) Load(addr uint16) uint8 {
	b.Address = addr
	b.RW = true
	b.Data = driveLoad(addr)
	return b.Data
}

// Store performs a Phi2 write cycle at addr on the drive's bus.
func (b *DriveBus) Store(addr uint16, val uint8) {
	b.Address = addr
	b.RW = false
	b.Data = val
	driveStore(addr, val)
}

// driveLoad reads addr through the 1541's fixed memory map: 2K RAM at
// $0000-$07FF, VIA1 mirrored across $1800-$1BFF, VIA2 mirrored across
// $1C00-$1FFF, and the 16K DOS ROM at $C000-$FFFF. Unmapped addresses read
// as open bus (approximated as $FF).
func driveLoad(addr uint16) uint8 {
	switch {
	case addr <= 0x07FF:
		return driveRAM[addr]
	case addr >= 0x1800 && addr <= 0x1BFF:
		if addr&0xF == 0x0 {
			// PRB has IEC-specific semantics, see iec.go.
			return via1ReadPRB()
		}
		return via1.Load(addr)
	case addr >= 0x1C00 && addr <= 0x1FFF:
		switch addr & 0xF {
		case 0x0:
			return via2ReadPRB()
		case 0x1:
			return via2ReadPRA()
		default:
			return via2.Load(addr)
		}
	case addr >= 0xC000:
		return rom.Drive1541[addr-0xC000]
	default:
		return 0xFF
	}
}

// driveStore writes addr through the 1541's fixed memory map. The DOS ROM
// is read-only; writes there are ignored, as are writes to unmapped
// addresses.
func driveStore(addr uint16, val uint8) {
	switch {
	case addr <= 0x07FF:
		driveRAM[addr] = val
	case addr >= 0x1800 && addr <= 0x1BFF:
		via1.Store(addr, val)
	case addr >= 0x1C00 && addr <= 0x1FFF:
		if addr&0xF == 0x0 {
			via2StorePRB(val)
		}
		via2.Store(addr, val)
	}
}

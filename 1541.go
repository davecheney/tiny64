package tiny64

import "github.com/davecheney/tiny64/rom"

// drive1541 is the full 1541 as an IEC peripheral: a 6502 running the DOS
// ROM, two VIAs, and a GCR read head. Its bus lines come straight from
// VIA1's port B (see iec.go).
type drive1541 struct{}

func (*drive1541) iecCLKOut() bool  { return via1ClkOut() }
func (*drive1541) iecDATAOut() bool { return via1DataOut() }
func (*drive1541) iecTick()         { driveTickPhi2() }

// A real 1541 takes its address from two jumpers on the board, giving 8
// through 11. Nothing here reads them, so the drive is always the first
// one.
func (*drive1541) iecAddress() uint8 { return 8 }

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

// driveAttached reports whether a 1541 is plugged into the IEC bus. A real
// C64 works perfectly well without one - and stepping a second CPU costs
// as much again as stepping the C64's - so nothing gets a drive until it
// asks for one with AttachDrive(true) or puts a disk in with InsertDisk.
// Until then the KERNAL simply reports DEVICE NOT PRESENT, exactly as a
// bare machine with nothing on the serial bus does.
var driveAttached bool

// AttachDrive connects or disconnects the 1541 from the IEC bus. A drive
// that is connected is reset immediately, as it would be at power-on.
func AttachDrive(attached bool) {
	driveAttached = attached
	if attached {
		attachIEC(&drive1541{})
		ResetDrive()
		return
	}
	detachIEC(&drive1541{})
}

// DriveAttached reports whether the 1541 is currently connected.
func DriveAttached() bool { return driveAttached }

// driveTickPhi2 advances the drive by one of its Phi2 cycles. The 1541 has
// its own 16MHz crystal and so runs asynchronously from the C64, but the
// two Phi2 rates are within 1.5% of each other (1.0MHz vs PAL's 985248Hz)
// and the IEC bus is fully handshaked, so clocking the drive from the same
// cycle as the C64's CPU is both simpler and close enough.
func driveTickPhi2() {
	if driveAttached {
		driveCPU.TickPhi2()
	}
}

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

// DriveRAM returns the drive's 2K of static RAM, for debugging/tracing
// tools that want to look at the DOS's variables.
func DriveRAM() []byte { return driveRAM[:] }

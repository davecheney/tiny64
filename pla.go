package tiny64

import "github.com/davecheney/tiny64/rom"

// The PLA emulates the C64's memory-decode logic. Real hardware wires it as
// a function of chip-enable lines; here it works directly in terms of
// addresses, inspecting the CPU's LORAM/HIRAM/CHAREN bank-switching lines
// (and, eventually, the CIAs) to decide whether an address is backed by
// RAM, BASIC/KERNAL/character ROM, or I/O.
//
// The CPU and VIC-II have entirely separate views of memory - not just
// different banking rules, but different address decoders wired to
// different chip-select lines. They already reach the PLA via separate
// call paths (cpu.load/store vs the VIC's direct plaVICLoad calls), so
// that separation is modeled here as separate functions rather than a
// single AEC-gated access path.

// plaLoad reads addr through the memory map currently selected by the
// CPU's bank-switching lines.
func plaLoad(addr uint16) uint8 {
	loram, hiram, charen := cpu.bankBits()

	switch {
	case addr >= 0x8000 && addr <= 0x9FFF && cartridge.eightK() && cartridge.ROML && loram && hiram:
		// An 8K cartridge's /ROML image. The PLA only asserts /ROML when
		// both LORAM and HIRAM are high and the cartridge asserts /EXROM.
		return cartridge.ROM[addr-0x8000]
	case addr >= 0xA000 && addr <= 0xBFFF && loram && hiram:
		return rom.Basic[addr-0xA000]
	case addr >= 0xD000 && addr <= 0xDFFF && (loram || hiram):
		if charen {
			return ioLoad(addr)
		}
		return rom.Character[addr-0xD000]
	case addr >= 0xE000 && cartridge.ultimax() && cartridge.ROMH:
		// A cartridge wired for MAX mode overrides the KERNAL entirely,
		// regardless of hiram.
		return cartridge.ROM[addr-0xE000]
	case addr >= 0xE000 && hiram:
		return rom.Kernal[addr-0xE000]
	default:
		return ram[addr]
	}
}

// plaStore writes addr through the memory map currently selected by the
// CPU's bank-switching lines. RAM is always writable underneath BASIC/
// KERNAL ROM; character ROM is read-only (RAM is disabled behind it), and
// I/O is dispatched to whichever chip is selected. Cartridge ROM is not a
// case here for the same reason: a ROM chip cannot be written, and the PLA
// leaves the RAM underneath it enabled for writes. That is what lets the
// KERNAL's RAMTAS find a cartridge - it writes $55, reads the ROM byte
// back instead, and stops its memory walk there.
func plaStore(addr uint16, val uint8) {
	loram, hiram, charen := cpu.bankBits()

	switch {
	case addr >= 0xD000 && addr <= 0xDFFF && (loram || hiram):
		if charen {
			ioStore(addr, val)
		}
		// else: character ROM selected, read-only; RAM is disabled here.
	default:
		ram[addr] = val
	}
}

// This is inlined into the dot path, which is worth more than anything a
// statement added here is likely to do. It costs 58 against the inliner's
// budget of 80, so there is room - but it was 90 and a real call until
// two changes brought it under, and going back over is silent. Check with
// -gcflags=github.com/davecheney/tiny64=-m=2.
//
// plaVICLoad reads addr through the VIC-II's own view of memory (used for
// its c-access/g-access fetches): it ignores the CPU's LORAM/HIRAM/CHAREN
// banking entirely. CIA2 Port A bits 0-1 select one of four 16K video
// banks through an inverter. The character generator ROM is hard-wired
// only at $1000-$1FFF in bank 0 and $9000-$9FFF in bank 2. A
// cartridge wired for MAX mode also overrides the top 4K of every 16K
// quadrant ($3000, $7000, $B000, $F000) with its ROMH image, via a
// dedicated hardware path separate from the CPU's $E000-$FFFF ROMH window
// - this is how MAX-mode carts supply the VIC with custom character/
// bitmap data baked into the cartridge ROM itself. The ROMH chip only has
// 13 address pins, so the offset it presents is always addr&0x1FFF,
// regardless of which window asserted its chip-select.
func plaVICLoad(addr uint16) uint8 {
	addr = vic.bank | addr&0x3FFF
	if cartridge.ultimax() && cartridge.ROMH && addr&0x3000 == 0x3000 {
		return cartridge.ROM[addr&0x1FFF]
	}
	// Character ROM is only mapped for VIC character generator accesses
	// in text mode (BMM=0). In bitmap mode (BMM=1), VIC always reads RAM.
	//
	// It appears at $1000-$1FFF of banks 0 and 2, which is $1000-$1FFF and
	// $9000-$9FFF. Those are the same 4K block of the same half of each
	// 16K bank, so one mask decides it: bit 14 selects the half, bits
	// 13-12 the block within it, and bit 15 - which is what tells the two
	// banks apart - is not consulted at all.
	if vic.control1&0x20 == 0 && addr&0x7000 == 0x1000 {
		return rom.Character[addr&0x0FFF]
	}
	return ram[addr]
}

// plaVICSpriteLoad reads sprite pointer and pattern data through the VIC-II's
// memory view. Character ROM is not mapped for sprite accesses (s-accesses).
func plaVICSpriteLoad(addr uint16) uint8 {
	addr = vic.bank | addr&0x3FFF
	if cartridge.ultimax() && cartridge.ROMH && addr&0x3000 == 0x3000 {
		return cartridge.ROM[addr&0x1FFF]
	}
	return ram[addr]
}

// ioLoad/ioStore dispatch the $D000-$DFFF I/O region to the appropriate
// chip. Unimplemented I/O falls through to RAM.
func ioLoad(addr uint16) uint8 {
	switch {
	case addr <= 0xD3FF:
		return vic.ReadRegister(addr)
	case addr >= 0xD800 && addr <= 0xDBFF:
		// The 2114 is only 4 bits wide; its unconnected upper data lines
		// float high, so reads report 1s there (matching VICE's model of
		// the chip).
		return colorRAM[addr-0xD800] | 0xF0
	case addr >= 0xDC00 && addr <= 0xDCFF:
		switch addr & 0xF {
		case 0x0:
			// PRA/PRB read back the keyboard matrix, see keyboard.go.
			return cia1ReadPRA()
		case 0x1:
			return cia1ReadPRB()
		}
		return cia.cia1.load(addr, sourceCIA1)
	case addr >= 0xDD00 && addr <= 0xDDFF:
		if addr&0xF == 0x0 {
			// PRA has IEC-specific semantics, see iec.go.
			return cia2ReadPRA()
		}
		return cia.cia2.load(addr, sourceCIA2)
	default:
		return ram[addr]
	}
}

func ioStore(addr uint16, val uint8) {
	switch {
	case addr <= 0xD3FF:
		vic.WriteRegister(addr, val)
	case addr >= 0xD800 && addr <= 0xDBFF:
		colorRAM[addr-0xD800] = val & 0x0F
	case addr >= 0xDC00 && addr <= 0xDCFF:
		cia.cia1.store(addr, val, sourceCIA1)
	case addr >= 0xDD00 && addr <= 0xDDFF:
		cia.cia2.store(addr, val, sourceCIA2)
		// Port A carries ATN, CLOCK OUT and DATA OUT, and its direction
		// register gates them. This is the only place the C64 can reach
		// the serial bus, so it is where the bus is woken. The same two
		// registers carry the VIC-II's bank-select lines, so this is also
		// the only place its fetch window can move.
		if r := addr & 0x0F; r == 0x00 || r == 0x02 {
			iecActive = true
			vic.setBank()
		}
	default:
		ram[addr] = val
	}
}

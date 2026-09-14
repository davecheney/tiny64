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
		// both LORAM and HIRAM are high, which is why software banks a
		// cartridge out by clearing LORAM rather than by anything the
		// cartridge itself provides.
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
	// CIA2's two bank-select lines are inverted. The port pins float high
	// when configured as inputs, just as the real pull-ups do.
	bank := uint16(^effective(cia2.PRA, cia2.DDRA)&0x03) << 14
	addr = bank | addr&0x3FFF
	if cartridge.ultimax() && cartridge.ROMH && addr&0x3000 == 0x3000 {
		return cartridge.ROM[addr&0x1FFF]
	}
	// Character ROM is only mapped for VIC character generator accesses
	// in text mode (BMM=0). In bitmap mode (BMM=1), VIC always reads RAM.
	if vic.control1&0x20 == 0 && (addr >= 0x1000 && addr <= 0x1FFF || addr >= 0x9000 && addr <= 0x9FFF) {
		return rom.Character[addr&0x0FFF]
	}
	return ram[addr]
}

// plaVICSpriteLoad reads sprite pointer and pattern data through the VIC-II's
// memory view. Character ROM is not mapped for sprite accesses (s-accesses).
func plaVICSpriteLoad(addr uint16) uint8 {
	bank := uint16(^effective(cia2.PRA, cia2.DDRA)&0x03) << 14
	addr = bank | addr&0x3FFF
	if cartridge.ultimax() && cartridge.ROMH && addr&0x3000 == 0x3000 {
		return cartridge.ROM[addr&0x1FFF]
	}
	return ram[addr]
}

// ioLoad/ioStore dispatch the $D000-$DFFF I/O region to the appropriate
// chip. The SID and cartridge I/O are ignored for now and simply fall
// through to RAM.
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
		return cia1.Load(addr)
	case addr >= 0xDD00 && addr <= 0xDDFF:
		if addr&0xF == 0x0 {
			// PRA has IEC-specific semantics, see iec.go.
			return cia2ReadPRA()
		}
		return cia2.Load(addr)
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
		cia1.Store(addr, val)
	case addr >= 0xDD00 && addr <= 0xDDFF:
		cia2.Store(addr, val)
	default:
		ram[addr] = val
	}
}

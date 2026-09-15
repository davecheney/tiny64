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

// What the PLA selects for a CPU access never varies within a 256-byte
// page: every boundary in the map ($8000, $A000, $C000, $D000, $E000) is
// page aligned. The decode is therefore a table lookup rather than a chain
// of range tests re-evaluated on every bus cycle, and Cartridge.pages holds
// the answer for all 256 pages in each of the eight states the CPU's three
// bank-switching lines can take.
const (
	// pageInvalid is the zero value, so a Cartridge that has never been
	// decoded - or has just been replaced wholesale - reads as "not built
	// yet" and gets rebuilt on the next access, rather than answering from
	// a table left over from different wiring. See Cartridge.pages.
	pageInvalid uint8 = iota

	// The only two kinds a write does not land in RAM under come first, so
	// that plaStore can separate them from the rest with one comparison.
	pageCharROM
	pageIO

	pageBasic
	pageKernal
	pageCartROML
	pageCartROMH

	// pageRAM comes last, and plaLoad answers it from the default arm of
	// its switch rather than a case of its own: the bounds check the
	// compiler emits ahead of the jump table for the banked kinds then
	// doubles as the test for the commonest kind of all, which is worth
	// having on a core with no branch predictor.
	pageRAM
)

// plaDecodePage rebuilds the page-decode table and answers from it. It is
// reached once after the cartridge wiring changes, and never otherwise, so
// it is kept out of line: plaLoad and plaStore do the lookup themselves,
// and TinyGo will not inline a caller that carries this with it.
//
//go:noinline
func plaDecodePage(addr uint16) uint8 {
	cartridge.decodePages()
	return cartridge.pages[cpu.bankSelect()][addr>>8]
}

// plaLoad reads addr through the memory map currently selected by the
// CPU's bank-switching lines.
//
// The table says what kind of memory a page holds, never where it is, so
// the ROM images stay where they are addressed rather than being reached
// through stored pointers.
func plaLoad(addr uint16) uint8 {
	kind := cartridge.pages[cpu.bankSelect()][addr>>8]
	if kind == pageInvalid {
		kind = plaDecodePage(addr)
	}
	switch kind {
	case pageKernal:
		return rom.Kernal[addr-0xE000]
	case pageBasic:
		return rom.Basic[addr-0xA000]
	case pageIO:
		return ioLoad(addr)
	case pageCharROM:
		return rom.Character[addr-0xD000]
	case pageCartROML:
		// An 8K cartridge's /ROML image. The PLA only asserts /ROML when
		// both LORAM and HIRAM are high and the cartridge asserts /EXROM.
		return cartridge.ROM[addr-0x8000]
	case pageCartROMH:
		// A cartridge wired for MAX mode overrides the KERNAL entirely,
		// regardless of hiram.
		return cartridge.ROM[addr-0xE000]
	default:
		// pageRAM. pageInvalid was resolved above, so nothing else reaches
		// here - and if a new kind ever did, reading RAM is the answer
		// that fails safe.
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
	kind := cartridge.pages[cpu.bankSelect()][addr>>8]
	if kind == pageInvalid {
		kind = plaDecodePage(addr)
	}
	if kind > pageIO {
		// RAM, or the RAM underneath BASIC, the KERNAL or cartridge ROM.
		ram[addr] = val
		return
	}
	if kind == pageIO {
		ioStore(addr, val)
	}
	// pageCharROM: read-only, and RAM is disabled behind it.
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
		return cia1.Load(addr)
	case addr >= 0xDD00 && addr <= 0xDDFF:
		if addr&0xF == 0x0 {
			// PRA has IEC-specific semantics, see iec.go.
			return cia2ReadPRA()
		}
		return cia2.Load(addr)
	case addr >= dosWedgeIO && cartridge.wedgeIO():
		return cartridge.ROM[dosWedgeIOBank+int(addr-dosWedgeIO)]
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
		// Port A carries ATN, CLOCK OUT and DATA OUT, and its direction
		// register gates them. This is the only place the C64 can reach
		// the serial bus, so it is where the bus is woken.
		if r := addr & 0x0F; r == 0x00 || r == 0x02 {
			iecActive = true
		}
	case addr >= dosWedgeIO && cartridge.wedgeIO():
		if addr == dosWedgeLatch {
			cartridge.writeWedgeLatch(val)
		}
	default:
		ram[addr] = val
	}
}

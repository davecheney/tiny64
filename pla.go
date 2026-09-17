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

// What the PLA selects for a CPU access never varies within a 4K block:
// every boundary in the map ($8000, $A000, $C000, $D000, $E000) is 4K
// aligned. So the decode is a sixteen-entry lookup rather than a chain of
// range tests re-evaluated on every bus cycle - and, since only five bits
// feed it (the CPU port's three bank-switching lines, plus the cartridge's
// two chip-selects as the /GAME and /EXROM wiring qualifies them), it is
// re-derived when those move rather than precomputed for every state they
// could take. Cartridge.blocks holds the map for the state the machine is
// in; Cartridge.blockSel says which state that is.
const (
	// The only two kinds a write does not land in RAM under come first, so
	// that plaStore can separate them from the rest with one comparison.
	blockCharROM uint8 = iota
	blockIO

	blockBasic
	blockKernal
	blockCartROML
	blockCartROMH

	// blockRAM comes last, and plaLoad answers it from the default arm of
	// its switch rather than a case of its own: the bounds check the
	// compiler emits ahead of the jump table for the banked kinds then
	// doubles as the test for the commonest kind of all, which is worth
	// having on a core with no branch predictor.
	blockRAM
)

// plaDecode re-derives the block decode for the bank-switching lines sel
// describes. It runs when those lines or the cartridge's wiring move, and
// never otherwise, so it is kept out of line: plaLoad and plaStore check
// the selector themselves, and TinyGo will not inline a caller that
// carries this with it.
//
//go:noinline
func plaDecode(sel uint8) {
	cartridge.decodeBlocks(sel)
}

// plaLoad reads addr through the memory map currently selected by the
// CPU's bank-switching lines.
//
// The map says what kind of memory a block holds, never where it is, so
// the ROM images stay where they are addressed rather than being reached
// through stored pointers - which would move 20K of embedded ROM out of
// flash and into RAM on the microcontroller targets.
func plaLoad(addr uint16) uint8 {
	sel := cpu.bankSelect()
	if cartridge.blockSel != sel+1 { // biased by one; see Cartridge.blocks
		plaDecode(sel)
	}
	switch cartridge.blocks[addr>>12] {
	case blockKernal:
		return rom.Kernal[addr-0xE000]
	case blockBasic:
		return rom.Basic[addr-0xA000]
	case blockIO:
		return ioLoad(addr)
	case blockCharROM:
		return rom.Character[addr-0xD000]
	case blockCartROML:
		return cartridge.ROM[addr-0x8000]
	case blockCartROMH:
		return cartridge.ROM[addr-0xE000]
	default:
		// blockRAM - and if a new kind ever reached here, reading RAM is
		// the answer that fails safe.
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
	sel := cpu.bankSelect()
	if cartridge.blockSel != sel+1 { // biased by one; see Cartridge.blocks
		plaDecode(sel)
	}
	kind := cartridge.blocks[addr>>12]
	if kind > blockIO {
		// RAM, or the RAM underneath BASIC, the KERNAL or cartridge ROM.
		ram[addr] = val
		return
	}
	if kind == blockIO {
		ioStore(addr, val)
	}
	// blockCharROM: read-only, and RAM is disabled behind it.
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

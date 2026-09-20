package tiny64

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"strings"
)

// The .crt container, as described by the VICE manual's file formats
// chapter. Everything in it is big-endian.
//
// A 64 byte file header carries a signature, the length of the header
// itself, a format version, the cartridge's hardware type, the state of
// the expansion port's /EXROM and /GAME lines, and a 32 byte name. The
// ROM follows in one or more CHIP packets, each with its own 16 byte
// header giving the chip's type, bank, load address and image size.
const (
	crtSignature  = "C64 CARTRIDGE   "
	crtHeaderSize = 0x40
	crtChipSize   = 0x10

	crtOffHeaderLen = 0x10
	crtOffHWType    = 0x16
	crtOffExrom     = 0x18
	crtOffGame      = 0x19
	crtOffName      = 0x20

	crtChipOffLength = 0x04
	crtChipOffType   = 0x08
	crtChipOffBank   = 0x0A
	crtChipOffLoad   = 0x0C
	crtChipOffSize   = 0x0E

	// Hardware type 0 is a "generic cartridge": plain ROM behind the
	// expansion port's own lines, with no mapper of its own. Every other
	// type is a bank-switching or freezer cartridge with registers tiny64
	// does not model.
	crtTypeGeneric = 0

	// Chip type 0 is ROM. The others are RAM, flash and EEPROM, which a
	// cartridge with a mapper uses and a generic one does not.
	crtChipTypeROM = 0
)

// Signatures for the other machines the container also serves, so a file
// meant for one of them can say so instead of being "not a cartridge".
var crtOtherMachines = map[string]string{
	"C128 CARTRIDGE  ": "C128",
	"CBM2 CARTRIDGE  ": "CBM-II",
	"VIC20 CARTRIDGE ": "VIC-20",
	"PLUS4 CARTRIDGE ": "C16/Plus4",
}

// CRT is a cartridge image read from a .crt file: the ROM, and the way
// the cartridge's PCB wires the expansion port.
type CRT struct {
	Name  string // the cartridge's name, from the file header
	ROM   []byte
	Game  bool // /GAME line: true = pulled low (asserted)
	Exrom bool // /EXROM line: true = pulled low (asserted)
	ROMH  bool // /ROMH chip-select populated with a physical ROM chip
	ROML  bool // /ROML chip-select populated with a physical ROM chip
}

// ReadCRT loads a .crt cartridge image from fsys.
func ReadCRT(fsys fs.FS, name string) (*CRT, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, err
	}
	c, err := ParseCRT(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", name, err)
	}
	return c, nil
}

// ParseCRT decodes a .crt cartridge image held in data. The returned
// CRT's ROM aliases data.
//
// Only the two wirings the PLA models are accepted: an ordinary 8K
// cartridge with a ROM chip on /ROML at $8000, and a MAX-mode cartridge
// with one on /ROMH at $E000. Everything else - bank-switching hardware,
// 16K cartridges, chips in windows tiny64 does not decode - is an error
// naming what is in the way, because a cartridge quietly mapped into the
// wrong window runs garbage rather than failing.
func ParseCRT(data []byte) (*CRT, error) {
	// One length check covers every fixed offset read below: the whole
	// file header, plus the smallest possible CHIP header after it.
	if len(data) < crtHeaderSize+crtChipSize {
		return nil, fmt.Errorf("tiny64: not a cartridge image: %d bytes, want at least %d", len(data), crtHeaderSize+crtChipSize)
	}
	if sig := string(data[:16]); sig != crtSignature {
		if machine, ok := crtOtherMachines[sig]; ok {
			return nil, fmt.Errorf("tiny64: %s cartridge image, not a C64 one", machine)
		}
		// The bytes themselves are not printed: a file that is not a
		// cartridge usually has binary there, and this goes to a terminal.
		return nil, fmt.Errorf("tiny64: not a cartridge image: does not begin with %q", crtSignature)
	}

	// The header length is authoritative, because a v1.1 or v2.0 header
	// may carry fields past the ones read here and the first CHIP packet
	// begins wherever it ends. Images exist in the wild that wrongly
	// store $20; the spec calls them out, and VICE reads them by treating
	// $40 as a floor. Seeking to $20 instead would land inside the name
	// field and read it as a CHIP header.
	headerLen := binary.BigEndian.Uint32(data[crtOffHeaderLen:])
	if headerLen < crtHeaderSize {
		headerLen = crtHeaderSize
	}
	// Compared in 64 bits: the field is a full uint32, and int is 32 bits
	// wide on the TinyGo targets this package also builds for.
	if uint64(headerLen) > uint64(len(data)) {
		return nil, fmt.Errorf("tiny64: cartridge header length %d is past the end of a %d byte image", headerLen, len(data))
	}

	// The version at $14 is deliberately not checked. v1.1 added a
	// subtype byte and v2.0 the other machines' signatures; neither
	// changes how a generic C64 cartridge is laid out, and rejecting them
	// would refuse files that are perfectly readable.
	if hw := binary.BigEndian.Uint16(data[crtOffHWType:]); hw != crtTypeGeneric {
		return nil, fmt.Errorf("tiny64: cartridge hardware type %d is not the generic type %d: bank-switching cartridges are not emulated", hw, crtTypeGeneric)
	}

	// In the container a line's status byte is 0 when the line is active,
	// which on the expansion port means pulled low. Cartridge records the
	// same fact the other way up, as "true = asserted", so the two
	// invert. Anything non-zero is inactive rather than only 1, because
	// that is the tolerant reading and costs nothing.
	crt := &CRT{
		Name:  crtName(data[crtOffName : crtOffName+32]),
		Exrom: data[crtOffExrom] == 0,
		Game:  data[crtOffGame] == 0,
	}

	chip, err := crtOnlyChip(data, int(headerLen))
	if err != nil {
		return nil, err
	}

	// Which chip-select the ROM sits behind follows from where the packet
	// says it loads, not from the line states: a MAX-mode cartridge may
	// legally populate /ROML at $8000, and deciding the window from the
	// lines alone would map such an image at $E000 and run whatever
	// happened to be at that offset.
	switch chip.load {
	case 0x8000:
		crt.ROML = true
	case 0xE000:
		crt.ROMH = true
	default:
		return nil, fmt.Errorf("tiny64: cartridge ROM loads at $%04X, which is not a window tiny64 decodes ($8000 for /ROML, $E000 for /ROMH)", chip.load)
	}

	// Both windows are 8K, and both are indexed with the low 13 bits of
	// the address - plaVICLoad reaches offset $1FFF whatever the chip's
	// real size is - so a short image would be read past its end.
	if len(chip.data) != 0x2000 {
		return nil, fmt.Errorf("tiny64: cartridge ROM is %d bytes, want %d: tiny64 models only 8K cartridge windows", len(chip.data), 0x2000)
	}
	crt.ROM = chip.data

	switch {
	case crt.ROML && !crt.eightK():
		return nil, fmt.Errorf("tiny64: cartridge loads at $8000 but is not wired as an ordinary 8K cartridge (/EXROM asserted, /GAME floating): %s", crtLines(crt))
	case crt.ROMH && !crt.ultimax():
		return nil, fmt.Errorf("tiny64: cartridge loads at $E000 but is not wired for MAX mode (/GAME asserted, /EXROM floating): %s", crtLines(crt))
	}
	return crt, nil
}

// eightK and ultimax ask the same questions of a parsed image that the
// PLA asks of the inserted cartridge, so the two cannot drift apart.
func (c *CRT) eightK() bool { return c.Exrom && !c.Game }
func (c *CRT) ultimax() bool {
	return c.Game && !c.Exrom
}

func crtLines(c *CRT) string {
	state := func(asserted bool) string {
		if asserted {
			return "asserted"
		}
		return "floating"
	}
	return fmt.Sprintf("/EXROM %s, /GAME %s", state(c.Exrom), state(c.Game))
}

// crtChip is one decoded CHIP packet.
type crtChip struct {
	load uint16
	data []byte
}

// crtOnlyChip walks the CHIP packets from off and returns the single one
// the cartridge is made of. tiny64 models one ROM chip per cartridge, so
// a second packet is an error rather than something to choose between -
// two packets is how a 16K cartridge is stored, and the PLA has no
// $A000-$BFFF cartridge case to serve one with.
func crtOnlyChip(data []byte, off int) (crtChip, error) {
	var chips []crtChip
	for off < len(data) {
		if len(data)-off < crtChipSize {
			return crtChip{}, fmt.Errorf("tiny64: cartridge has %d trailing bytes, too few for a CHIP packet header", len(data)-off)
		}
		if sig := string(data[off : off+4]); sig != "CHIP" {
			return crtChip{}, fmt.Errorf("tiny64: cartridge CHIP packet at offset %d has signature %q", off, sig)
		}

		// Guarded before it is used to advance: a zero would not move the
		// cursor and the walk would never end.
		if packetLen := binary.BigEndian.Uint32(data[off+crtChipOffLength:]); packetLen < crtChipSize {
			return crtChip{}, fmt.Errorf("tiny64: cartridge CHIP packet at offset %d claims to be %d bytes, shorter than its own %d byte header", off, packetLen, crtChipSize)
		}

		typ := binary.BigEndian.Uint16(data[off+crtChipOffType:])
		bank := binary.BigEndian.Uint16(data[off+crtChipOffBank:])
		load := binary.BigEndian.Uint16(data[off+crtChipOffLoad:])
		size := int(binary.BigEndian.Uint16(data[off+crtChipOffSize:]))

		if typ != crtChipTypeROM {
			return crtChip{}, fmt.Errorf("tiny64: cartridge CHIP packet at $%04X is chip type %d, not ROM", load, typ)
		}
		if bank != 0 {
			return crtChip{}, fmt.Errorf("tiny64: cartridge CHIP packet at $%04X is in bank %d: banked cartridges are not emulated", load, bank)
		}
		if size == 0 {
			return crtChip{}, fmt.Errorf("tiny64: cartridge CHIP packet at $%04X holds no ROM", load)
		}
		if off+crtChipSize+size > len(data) {
			return crtChip{}, fmt.Errorf("tiny64: cartridge CHIP packet at $%04X wants %d bytes of ROM, but the image ends %d bytes later", load, size, len(data)-off-crtChipSize)
		}

		chips = append(chips, crtChip{load: load, data: data[off+crtChipSize : off+crtChipSize+size]})
		if len(chips) > 1 {
			return crtChip{}, fmt.Errorf("tiny64: cartridge has more than one CHIP packet ($%04X and $%04X): tiny64 models one ROM chip per cartridge", chips[0].load, load)
		}

		// Advanced by the header plus the image rather than by the
		// packet's own length field, which is what VICE does: it reads a
		// fixed size chip header and then the image, and files exist
		// where the two disagree.
		off += crtChipSize + size
	}
	if len(chips) == 0 {
		return crtChip{}, fmt.Errorf("tiny64: cartridge has no CHIP packets")
	}
	return chips[0], nil
}

// crtName cleans up the container's 32 byte name field. It is variously
// NUL-padded, space-padded, or full to the last byte with no terminator,
// and dumps carry junk outside ASCII; the result goes on to a window
// title, which has to be valid UTF-8.
func crtName(b []byte) string {
	var s strings.Builder
	for _, c := range b {
		if c >= 0x20 && c < 0x7F {
			s.WriteByte(c)
		}
	}
	return strings.TrimSpace(s.String())
}

// InsertCRT plugs a cartridge read from a .crt file into the expansion
// port.
//
// Mapping changes immediately; call Reset before running the machine, so
// the CPU takes its reset vector through the cartridge's own mapping.
func (b *Bus) InsertCRT(c *CRT) {
	b.Insert(c.ROM, c.Game, c.Exrom, c.ROMH, c.ROML)
}

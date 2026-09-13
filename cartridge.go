package tiny64

import (
	"encoding/binary"
	"fmt"
	"os"
)

// Cartridge holds the state of whatever is plugged into the expansion
// port. The zero value matches an empty port: /GAME and /EXROM float high
// (unasserted), so the PLA falls back to normal CPU-port-driven banking.
type Cartridge struct {
	ROM          []byte
	ROMLData     []byte
	ROMHData     []byte
	Name         string
	HardwareType uint16
	Game         bool // /GAME line: true = pulled low (asserted)
	Exrom        bool // /EXROM line: true = pulled low (asserted)
	ROMH         bool // /ROMH chip-select populated with a physical ROM chip
	ROML         bool // /ROML chip-select populated with a physical ROM chip
}

var cartridge Cartridge

const (
	crtHeaderMagic = "C64 CARTRIDGE   "
	crtChipMagic   = "CHIP"

	crtHardwareNormal  = 0
	crtHardwareUltimax = 1
)

// ultimax reports whether the cartridge's /GAME and /EXROM lines are wired
// the way the DiSTestMAX build instructions describe: /GAME low, /EXROM
// high or floating. This overrides the CPU's LORAM/HIRAM/CHAREN banking
// entirely on real hardware, but for now tiny64 only special-cases the
// $E000-$FFFF KERNAL area (see plaLoad).
func (c *Cartridge) ultimax() bool {
	return c.Game && !c.Exrom
}

func (c *Cartridge) romlLoad(addr uint16) uint8 {
	rom := c.ROMLData
	if len(rom) == 0 {
		rom = c.ROM
	}
	if len(rom) == 0 {
		return 0xFF
	}
	return rom[int(addr)%len(rom)]
}

func (c *Cartridge) romhLoad(addr uint16) uint8 {
	rom := c.ROMHData
	if len(rom) == 0 {
		rom = c.ROM
	}
	if len(rom) == 0 {
		return 0xFF
	}
	return rom[int(addr)%len(rom)]
}

// Insert plugs a cartridge into the expansion port. rom is the raw ROM
// image; game/exrom wire the /GAME and /EXROM lines (true = pulled low,
// asserted); romh/roml indicate which of the cartridge's chip-selects are
// actually populated with a ROM chip on the PCB.
func (b *Bus) Insert(rom []byte, game, exrom, romh, roml bool) {
	cart := Cartridge{ROM: rom, Game: game, Exrom: exrom, ROMH: romh, ROML: roml}
	if roml {
		cart.ROMLData = rom
	}
	if romh {
		cart.ROMHData = rom
	}
	b.InsertCartridge(cart)
}

// InsertCartridge plugs a parsed cartridge into the expansion port.
func (b *Bus) InsertCartridge(cart Cartridge) {
	cartridge = cart
}

// Remove unplugs the cartridge, restoring /GAME and /EXROM to their
// floating (no cartridge present) state.
func (b *Bus) Remove() {
	cartridge = Cartridge{}
}

// ReadCartridge loads a CRT cartridge image from path.
func ReadCartridge(path string) (Cartridge, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Cartridge{}, err
	}
	return ParseCRT(data)
}

// ParseCRT parses a C64 CRT cartridge image.
func ParseCRT(data []byte) (Cartridge, error) {
	if len(data) < 0x40 {
		return Cartridge{}, fmt.Errorf("CRT image is %d bytes, shorter than 64-byte header", len(data))
	}
	if string(data[:16]) != crtHeaderMagic {
		return Cartridge{}, fmt.Errorf("CRT image has magic %q, want %q", data[:16], crtHeaderMagic)
	}

	headerLen := int(binary.BigEndian.Uint32(data[0x10:0x14]))
	if headerLen < 0x40 || headerLen > len(data) {
		return Cartridge{}, fmt.Errorf("CRT header length %d is outside image length %d", headerLen, len(data))
	}

	cart := Cartridge{
		HardwareType: binary.BigEndian.Uint16(data[0x16:0x18]),
		Exrom:        data[0x18] == 0,
		Game:         data[0x19] == 0,
		Name:         nulString(data[0x20:0x40]),
	}
	switch cart.HardwareType {
	case crtHardwareNormal:
	case crtHardwareUltimax:
		cart.Game, cart.Exrom = true, false
	default:
		return Cartridge{}, fmt.Errorf("unsupported CRT hardware type %d", cart.HardwareType)
	}

	for off := headerLen; off < len(data); {
		if off+0x10 > len(data) {
			return Cartridge{}, fmt.Errorf("truncated CRT CHIP header at offset %d", off)
		}
		if string(data[off:off+4]) != crtChipMagic {
			return Cartridge{}, fmt.Errorf("CRT packet at offset %d has magic %q, want %q", off, data[off:off+4], crtChipMagic)
		}
		packetLen := int(binary.BigEndian.Uint32(data[off+4 : off+8]))
		if packetLen < 0x10 || off+packetLen > len(data) {
			return Cartridge{}, fmt.Errorf("CRT CHIP packet at offset %d has invalid length %d", off, packetLen)
		}
		chipType := binary.BigEndian.Uint16(data[off+8 : off+0x0A])
		bank := binary.BigEndian.Uint16(data[off+0x0A : off+0x0C])
		start := binary.BigEndian.Uint16(data[off+0x0C : off+0x0E])
		size := int(binary.BigEndian.Uint16(data[off+0x0E : off+0x10]))
		if size != packetLen-0x10 {
			return Cartridge{}, fmt.Errorf("CRT CHIP packet at offset %d has image size %d but packet carries %d bytes", off, size, packetLen-0x10)
		}
		if chipType != 0 {
			return Cartridge{}, fmt.Errorf("unsupported CRT CHIP type %d at offset %d", chipType, off)
		}
		if bank != 0 {
			return Cartridge{}, fmt.Errorf("unsupported banked CRT CHIP bank %d at offset %d", bank, off)
		}

		chip := data[off+0x10 : off+packetLen]
		switch start {
		case 0x8000:
			if size > 0x2000 {
				cart.ROMLData = append([]byte(nil), chip[:0x2000]...)
				cart.ROML = true
				cart.ROMHData = append([]byte(nil), chip[0x2000:]...)
				cart.ROMH = true
			} else {
				cart.ROMLData = append([]byte(nil), chip...)
				cart.ROML = true
			}
		case 0xA000, 0xE000:
			cart.ROMHData = append([]byte(nil), chip...)
			cart.ROMH = true
		default:
			return Cartridge{}, fmt.Errorf("unsupported CRT CHIP start address %#04x at offset %d", start, off)
		}
		off += packetLen
	}

	if !cart.ROML && !cart.ROMH {
		return Cartridge{}, fmt.Errorf("CRT image contains no ROML or ROMH CHIP packets")
	}
	switch {
	case cart.ROML && cart.ROMH:
		cart.ROM = append(append([]byte(nil), cart.ROMLData...), cart.ROMHData...)
	case cart.ROML:
		cart.ROM = append([]byte(nil), cart.ROMLData...)
	case cart.ROMH:
		cart.ROM = append([]byte(nil), cart.ROMHData...)
	}
	return cart, nil
}

func nulString(buf []byte) string {
	for i, b := range buf {
		if b == 0 {
			return string(buf[:i])
		}
	}
	return string(buf)
}

package tiny64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

// Cartridge holds the state of whatever is plugged into the expansion
// port. The zero value matches an empty port: /GAME and /EXROM float high
// (unasserted), so the PLA falls back to normal CPU-port-driven banking.
type Cartridge struct {
	ROMLData     []byte // bytes exposed through the /ROML chip-select, usually $8000-$9FFF
	ROMHData     []byte // bytes exposed through the /ROMH chip-select, usually $A000-$BFFF or $E000-$FFFF
	Name         string // cartridge name from the CRT header
	HardwareType uint16 // raw CRT hardware type from the CRT header
	Game         bool   // /GAME line: true = pulled low (asserted)
	Exrom        bool   // /EXROM line: true = pulled low (asserted)
	ROMH         bool   // /ROMH chip-select populated with a physical ROM chip
	ROML         bool   // /ROML chip-select populated with a physical ROM chip
}

var cartridge Cartridge

const (
	crtHeaderMagic = "C64 CARTRIDGE   "
	crtChipMagic   = "CHIP"

	cartridgeBankSize = 0x2000

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
	return cartridgeLoad(c.ROMLData, addr)
}

func (c *Cartridge) romhLoad(addr uint16) uint8 {
	return cartridgeLoad(c.ROMHData, addr)
}

func cartridgeLoad(rom []byte, addr uint16) uint8 {
	if len(rom) == 0 {
		return 0xFF
	}
	// Real cartridge ROM chips expose only the address pins they have. A
	// smaller image therefore naturally mirrors through the selected window.
	if isPowerOfTwo(len(rom)) {
		return rom[int(addr)&(len(rom)-1)]
	}
	return rom[int(addr)%len(rom)]
}

func splitROMLH(rom []byte) (roml, romh []byte) {
	if len(rom) > cartridgeBankSize {
		return rom[:cartridgeBankSize], rom[cartridgeBankSize:]
	}
	return rom, nil
}

func isPowerOfTwo(n int) bool {
	return n > 0 && n&(n-1) == 0
}

// Insert plugs a cartridge into the expansion port. rom is the raw ROM
// image; game/exrom wire the /GAME and /EXROM lines (true = pulled low,
// asserted); romh/roml indicate which of the cartridge's chip-selects are
// actually populated with a ROM chip on the PCB.
func (b *Bus) Insert(rom []byte, game, exrom, romh, roml bool) {
	cart := Cartridge{Game: game, Exrom: exrom, ROMH: romh, ROML: roml}
	if roml && romh {
		cart.ROMLData, cart.ROMHData = splitROMLH(rom)
		if cart.ROMHData == nil {
			cart.ROMHData = rom
		}
	} else if roml {
		cart.ROMLData = rom
	} else if romh {
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
		// Hardware type 1 defines the cartridge as Ultimax/MAX mode, so
		// trust the hardware type even if the header line bytes disagree.
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
		if chipType != 0 {
			return Cartridge{}, fmt.Errorf("unsupported CRT CHIP type %d at offset %d", chipType, off)
		}
		if bank != 0 {
			return Cartridge{}, fmt.Errorf("unsupported banked CRT CHIP bank %d at offset %d", bank, off)
		}
		if size != packetLen-0x10 {
			return Cartridge{}, fmt.Errorf("CRT CHIP packet at offset %d has image size %d but packet carries %d bytes", off, size, packetLen-0x10)
		}
		if size > 0x4000 {
			return Cartridge{}, fmt.Errorf("CRT CHIP packet at offset %d has unsupported image size %d", off, size)
		}

		chip := data[off+0x10 : off+packetLen]
		switch start {
		case 0x8000:
			if size > cartridgeBankSize {
				// 16K normal CRT images can store both ROML and ROMH in one
				// CHIP packet starting at $8000: the first 8K is ROML, the
				// remainder is ROMH.
				roml, romh := splitROMLH(chip)
				cart.ROMLData = append([]byte(nil), roml...)
				cart.ROML = true
				cart.ROMHData = append([]byte(nil), romh...)
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
		return Cartridge{}, errors.New("CRT image contains no ROML or ROMH CHIP packets")
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

package tiny64

import (
	"testing"

	"github.com/davecheney/tiny64/rom"
)

// TestPLAVICLoadCharacterMemory checks, for every address the VIC-II's
// 14-bit address bus can reach, that plaVICLoad permits exactly what
// real hardware wires up: the character generator ROM at $1000-$1FFF
// (hard-wired into the VIC's view regardless of CPU banking), and RAM
// everywhere else - including the ranges cartridges/software point a
// custom character base at (e.g. DiSTestMAX's $3800) to redefine the
// character set without needing a cartridge at all.
func TestPLAVICLoadCharacterMemory(t *testing.T) {
	saveMachine(t)

	const ramMarker = 0xAA
	for i := range ram {
		ram[i] = ramMarker
	}

	for addr := 0; addr < 0x4000; addr++ {
		got := plaVICLoad(uint16(addr))
		if addr >= 0x1000 && addr <= 0x1FFF {
			want := rom.Character[addr-0x1000]
			if got != want {
				t.Fatalf("VICLoad(%#04x) = %#02x, want character ROM byte %#02x", addr, got, want)
			}
			continue
		}
		if got != ramMarker {
			t.Fatalf("VICLoad(%#04x) = %#02x, want RAM marker %#02x (custom character data must be visible here)", addr, got, ramMarker)
		}
	}
}

// TestPLAVICLoadUltimaxROMH checks that a MAX-mode cartridge's ROMH image
// overrides the top 4K of every 16K quadrant ($3000, $7000, $B000, $F000)
// in the VIC-II's view - the dedicated hardware path (separate from the
// CPU's $E000-$FFFF ROMH window) that lets a MAX-mode cartridge supply the
// VIC with character/bitmap data baked into its ROM. The ROMH chip only
// has 13 address pins, so it presents the same addr&0x1FFF offset
// regardless of which window asserted its chip-select.
func TestPLAVICLoadUltimaxROMH(t *testing.T) {
	saveMachine(t)

	const ramMarker = 0xBB
	for i := range ram {
		ram[i] = ramMarker
	}

	cartROM := make([]byte, 0x2000)
	for i := range cartROM {
		cartROM[i] = byte(i)
	}
	cartridge = Cartridge{ROM: cartROM, Game: true, Exrom: false, ROMH: true}

	for _, base := range []uint16{0x3000, 0x7000, 0xB000, 0xF000} {
		for offset := 0; offset <= 0x0FFF; offset++ {
			addr := base + uint16(offset)
			want := cartROM[addr&0x1FFF]
			if got := plaVICLoad(addr); got != want {
				t.Fatalf("VICLoad(%#04x) = %#02x, want cartridge ROMH byte %#02x", addr, got, want)
			}
		}
	}

	// Outside those windows, RAM (or the on-board character ROM) is
	// unaffected by the cartridge.
	if got := plaVICLoad(0x2FFF); got != ramMarker {
		t.Fatalf("VICLoad(0x2fff) = %#02x, want RAM marker %#02x", got, ramMarker)
	}
	if got := plaVICLoad(0x4000); got != ramMarker {
		t.Fatalf("VICLoad(0x4000) = %#02x, want RAM marker %#02x (cartridge ROMH must not leak past $3fff)", got, ramMarker)
	}
}

// TestPLALoadCartridge8KROML checks the banking rules for an ordinary 8K
// cartridge: /EXROM asserted, /GAME floating, a ROM chip on /ROML. The
// image appears at $8000-$9FFF, and only while the CPU is driving both
// LORAM and HIRAM high - clearing either one is how software banks a
// cartridge out and gets at the RAM underneath.
func TestPLALoadCartridge8KROML(t *testing.T) {
	saveMachine(t)

	cartROM := make([]byte, 0x2000)
	for i := range cartROM {
		cartROM[i] = byte(i)
	}
	cartridge = Cartridge{ROM: cartROM, Game: false, Exrom: true, ROML: true}

	const ramMarker = 0xCC
	for i := range ram {
		ram[i] = ramMarker
	}

	// A cold CPU leaves every port pin floating high, which is LORAM,
	// HIRAM and CHAREN all set - the state IOINIT puts the port in too.
	cpu = CPU{}
	for offset := 0; offset <= 0x1FFF; offset++ {
		addr := uint16(0x8000 + offset)
		if got, want := plaLoad(addr), cartROM[offset]; got != want {
			t.Fatalf("load(%#04x) = %#02x, want cartridge ROML byte %#02x", addr, got, want)
		}
	}

	// The cartridge occupies its window and nothing else: BASIC and the
	// KERNAL are still where they were, and so is the RAM either side.
	if got, want := plaLoad(0xA000), rom.Basic[0]; got != want {
		t.Errorf("load(0xa000) = %#02x, want BASIC ROM byte %#02x", got, want)
	}
	if got, want := plaLoad(0xE000), rom.Kernal[0]; got != want {
		t.Errorf("load(0xe000) = %#02x, want KERNAL ROM byte %#02x", got, want)
	}
	if got := plaLoad(0x7FFF); got != ramMarker {
		t.Errorf("load(0x7fff) = %#02x, want RAM marker %#02x", got, ramMarker)
	}

	// Writes are absorbed by the RAM underneath rather than modifying the
	// ROM or being dropped. This is what the KERNAL's RAMTAS relies on to
	// notice a cartridge: it writes $55, reads a ROM byte back, and stops
	// its memory walk there, which is what sets the top of BASIC memory.
	plaStore(0x8000, 0x55)
	if got := ram[0x8000]; got != 0x55 {
		t.Errorf("after store, ram[0x8000] = %#02x, want %#02x", got, 0x55)
	}
	if got, want := plaLoad(0x8000), cartROM[0]; got != want {
		t.Errorf("after store, load(0x8000) = %#02x, want cartridge ROM byte %#02x", got, want)
	}
	ram[0x8000] = ramMarker

	// Driving the port for real: either bank-select line low takes the
	// cartridge out of the map.
	cpu.PortDDR = 0xFF
	for _, port := range []struct {
		val  uint8
		name string
	}{
		{0x36, "LORAM low"},
		{0x35, "HIRAM low"},
		{0x34, "LORAM and HIRAM low"},
	} {
		cpu.Port = port.val
		if got := plaLoad(0x8000); got != ramMarker {
			t.Errorf("with %s, load(0x8000) = %#02x, want RAM marker %#02x", port.name, got, ramMarker)
		}
	}
	cpu.Port = 0x37
	if got, want := plaLoad(0x8000), cartROM[0]; got != want {
		t.Errorf("with LORAM and HIRAM high, load(0x8000) = %#02x, want cartridge ROM byte %#02x", got, want)
	}
}

// TestPLALoadEmptyPortLeavesRAMAt8000 checks that an empty expansion port
// leaves $8000-$9FFF as plain RAM, so a machine booted without a cartridge
// is unaffected by the ROML window existing at all.
func TestPLALoadEmptyPortLeavesRAMAt8000(t *testing.T) {
	saveMachine(t)

	bus.Remove()
	cpu = CPU{}
	ram[0x8000] = 0xA5
	if got := plaLoad(0x8000); got != 0xA5 {
		t.Errorf("load(0x8000) = %#02x, want RAM %#02x", got, 0xA5)
	}

	// A MAX-mode cartridge populates /ROMH, not /ROML, so it must not
	// appear in this window either.
	cartridge = Cartridge{ROM: make([]byte, 0x2000), Game: true, Exrom: false, ROMH: true}
	if got := plaLoad(0x8000); got != 0xA5 {
		t.Errorf("with a MAX-mode cartridge, load(0x8000) = %#02x, want RAM %#02x", got, 0xA5)
	}
}

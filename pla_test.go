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

func TestPLAVICLoadRespectsCIA2VideoBank(t *testing.T) {
	saveMachine(t)

	for bank := uint16(0); bank < 4; bank++ {
		ram[bank<<14] = byte(0xA0 + bank)
	}

	cia2.DDRA = 0x03
	for bank := uint8(0); bank < 4; bank++ {
		// CIA2's two video-bank outputs are inverted by the board logic.
		cia2.PRA = ^bank & 0x03
		if got, want := plaVICLoad(0), byte(0xA0+bank); got != want {
			t.Errorf("VIC bank %d load = %#02x, want %#02x", bank, got, want)
		}
	}

	// The character ROM is physically decoded only in banks 0 and 2.
	ram[0x5000] = 0x5A
	cia2.PRA = 0x02 // inverted bank selection: bank 1
	if got := plaVICLoad(0x1000); got != 0x5A {
		t.Errorf("bank 1 character window = %#02x, want RAM %#02x", got, 0x5A)
	}
}

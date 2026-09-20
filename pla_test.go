package tiny64

import (
	"testing"

	"github.com/davecheney/tiny64/rom"
)

// TestPLAVICLoadCharacterMemory checks, for every address the VIC-II's
// 14-bit address bus can reach, that plaVICLoad permits exactly what
// real hardware wires up: the character generator ROM at $1000-$1FFF
// (hard-wired into the VIC's view regardless of CPU banking), and RAM
// everywhere else - including the ranges software points a custom
// character base at (e.g. $3800) to redefine the character set.
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

// TestPLALoadRAMAt8000 checks that $8000-$9FFF is plain RAM. With no
// expansion port modelled there is nothing that can map over it.
func TestPLALoadRAMAt8000(t *testing.T) {
	saveMachine(t)

	cpu = CPU{}
	ram[0x8000] = 0xA5
	if got := plaLoad(0x8000); got != 0xA5 {
		t.Errorf("load(0x8000) = %#02x, want RAM %#02x", got, 0xA5)
	}
}

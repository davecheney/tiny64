package tiny64

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"

	"github.com/davecheney/tiny64/rom"
)

// This fixture is deliberately not distributed with the emulator. Run each
// subtest in a fresh process with TINY64_UNCLE_ANGUS_PRG pointing at the PRG.
func TestDOSWedgeUncleAngus(t *testing.T) {
	skipShort(t)

	path := os.Getenv("TINY64_UNCLE_ANGUS_PRG")
	if path == "" {
		t.Skip("set TINY64_UNCLE_ANGUS_PRG to run the local compatibility check")
	}
	prg, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const hash = "470e39185204de92bbfce533a883c45053cd22ec74877ff913e5ab8ec3998216"
	if len(prg) != 9965 || fmt.Sprintf("%x", sha256.Sum256(prg)) != hash {
		t.Fatal("fixture is not the expected Uncle Angus McFungus PRG")
	}
	// $F5A9 is the KERNAL's successful LOAD tail, before BASIC can relink
	// or execute the program. Observing it does not alter CPU execution.
	if !bytes.Equal(rom.Kernal[0x15A9:0x15AF], []byte{0x18, 0xA6, 0xAE, 0xA4, 0xAF, 0x60}) {
		t.Fatal("KERNAL LOAD completion address changed")
	}
	for _, mode := range []string{"stock", "load-run", "up-arrow"} {
		t.Run(mode, func(t *testing.T) {
			m := newMachine(t)
			useDrive(t, virtualDriveDisk(t, "UNCLE", prg))
			if mode != "stock" {
				EnableDOSWedge()
				m.reset(5, "READY.")
			} else {
				m.waitForLine(5, "READY.")
			}
			switch mode {
			case "stock":
				m.typeLine(`LOAD"*",8`)
			case "load-run":
				m.typeLine("/*")
			case "up-arrow":
				m.typeLine("↑*")
			}
			m.runUntil("KERNAL LOAD completion", 80_000_000, func() bool {
				return cpu.PC == 0xF5A9 && cpu.TState == 0
			})
			end := 0x0801 + len(prg) - 2
			if !bytes.Equal(ram[0x0801:end], prg[2:]) {
				t.Fatal("KERNAL did not load the PRG byte for byte")
			}
			if cartridge.Exrom {
				t.Fatal("ROML is mapped at LOAD completion")
			}
			if mode != "up-arrow" {
				waitForLoad(m)
				for _, key := range []Key{KeyR, KeyU, KeyN} {
					m.press(false, key)
				}
				keyboard.Press(KeyReturn)
			}
			m.runUntil("decompressor entry", 2_000_000, func() bool {
				return cpu.PC == 0x080D && cpu.TState == 0
			})
			keyboard.ReleaseAll()
			// Observe the formerly failing read, then allow startup to settle
			// before running another 1000 PAL frames.
			m.runUntil("decompressor read at $9F9A", 2_000_000, func() bool {
				return bus.RW && bus.Address == 0x9F9A
			})
			if bus.Data != ram[0x9F9A] || cartridge.Exrom {
				t.Fatalf("$9F9A read = $%02X, RAM = $%02X, EXROM=%v", bus.Data, ram[0x9F9A], cartridge.Exrom)
			}
			m.runUntil("decompressed entry at $0900", 20_000_000, func() bool {
				return cpu.PC == 0x0900 && cpu.TState == 0
			})
			const unpackedHash = "6b00116eea69bc69ef15517637d2cfc68d02a2cb8eb9cdcd716e6344655ff2a4"
			if got := fmt.Sprintf("%x", sha256.Sum256(ram[0x0900:0xC000])); got != unpackedHash {
				t.Fatalf("decompressed RAM SHA256=%s, want stock result %s", got, unpackedHash)
			}
			for range 1100 {
				vic.StepFrame()
				if cartridge.Exrom {
					t.Fatal("cartridge mapped ROML during game execution")
				}
			}
			t.Logf("loaded %d bytes; $9F9A read RAM; ran 100 settling + 1000 further frames; PC=$%04X EXROM=%v",
				len(prg)-2, cpu.PC, cartridge.Exrom)
		})
	}
}

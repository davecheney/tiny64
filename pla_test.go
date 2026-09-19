package tiny64

import (
	"bytes"
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

	// Through the I/O path a program would use, not by poking the port:
	// the window the VIC fetches through is worked out where a write to
	// this port lands, so an assignment to the field alone would leave it
	// describing the previous bank.
	ioStore(0xDD02, 0x03)
	for bank := uint8(0); bank < 4; bank++ {
		// CIA2's two video-bank outputs are inverted by the board logic.
		ioStore(0xDD00, ^bank&0x03)
		if got, want := plaVICLoad(0), byte(0xA0+bank); got != want {
			t.Errorf("VIC bank %d load = %#02x, want %#02x", bank, got, want)
		}
	}

	// The character ROM is physically decoded only in banks 0 and 2.
	ram[0x5000] = 0x5A
	ioStore(0xDD00, 0x02) // inverted bank selection: bank 1
	if got := plaVICLoad(0x1000); got != 0x5A {
		t.Errorf("bank 1 character window = %#02x, want RAM %#02x", got, 0x5A)
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

	// A MAX-mode cartridge populates /ROMH, not /ROML.
	cartridge = Cartridge{ROM: make([]byte, 0x2000), Game: true, Exrom: false, ROMH: true}
	if got := plaLoad(0x8000); got != 0xA5 {
		t.Errorf("with a MAX-mode cartridge, load(0x8000) = %#02x, want RAM %#02x", got, 0xA5)
	}
}

func TestPLAWedgeLatch(t *testing.T) {
	saveMachine(t)
	cpu = CPU{PortDDR: 0x2F, Port: 0xF7}
	EnableDOSWedge()
	image := append([]byte(nil), cartridge.ROM...)
	for addr := uint16(0x8000); addr < 0xA000; addr++ {
		bus.Store(addr, byte(addr>>8))
		if got := bus.Load(addr); got != image[addr-0x8000] {
			t.Fatalf("mapped ROM at $%04X = $%02X", addr, got)
		}
	}
	for addr := uint16(dosWedgeIO); ; addr++ {
		bus.Store(addr, 0) // only $DFFF is a latch; the aperture is read-only
		if got, want := bus.Load(addr), image[dosWedgeIOBank+int(addr-dosWedgeIO)]; got != want {
			t.Fatalf("I/O ROM at $%04X = $%02X, want $%02X", addr, got, want)
		}
		if addr == dosWedgeLatch {
			break
		}
		if !cartridge.Exrom {
			t.Fatalf("write to $%04X changed the latch", addr)
		}
	}
	assertWedgeRAM(t)
	bus.Store(dosWedgeLatch, dosWedgeMap)
	if !cartridge.Exrom {
		t.Fatal("write did not assert EXROM")
	}
	bus.Store(dosWedgeLatch, dosWedgeKill|dosWedgeMap)
	assertWedgeRAM(t)
	for value := 0; value < 256; value++ {
		bus.Store(dosWedgeLatch, byte(value))
		if cartridge.Exrom || !cartridge.killed {
			t.Fatalf("value $%02X reactivated killed cartridge", value)
		}
	}
	Reset()
	if cartridge.killed || !cartridge.Exrom || bus.Load(0x8004) != 0xC3 {
		t.Fatal("hardware reset did not restore CBM80 mapping")
	}
	if !bytes.Equal(cartridge.ROM, image) {
		t.Fatal("writes changed the cartridge ROM")
	}
}

func TestPLAWedgeLatchRequiresIO(t *testing.T) {
	saveMachine(t)
	for bits := byte(0); bits < 8; bits++ {
		EnableDOSWedge()
		cpu = CPU{PortDDR: 7, Port: bits}
		bus.Store(dosWedgeLatch, dosWedgeKill)
		want := bits&4 != 0 && bits&3 != 0
		if cartridge.killed != want {
			t.Fatalf("port bits=%03b, killed=%v, want %v", bits, cartridge.killed, want)
		}
	}
	EnableDOSWedge()
	cpu = CPU{PortDDR: 0, Port: 0}
	bus.Store(dosWedgeLatch, dosWedgeKill)
	if !cartridge.killed {
		t.Fatal("floating port inputs must select I/O")
	}
}

func TestPLAWedgeLatchIsCartridgeSpecific(t *testing.T) {
	saveMachine(t)
	for _, kind := range []string{"empty", "8K", "ultimax", "same-image"} {
		t.Run(kind, func(t *testing.T) {
			// Replacing a wedge must discard its latch even when the raw ROM
			// happens to be the same. Bus.Insert is an ordinary cartridge.
			EnableDOSWedge()
			switch kind {
			case "empty":
				bus.Remove()
			case "8K":
				bus.Insert(make([]byte, 8192), false, true, false, true)
			case "ultimax":
				bus.Insert(make([]byte, 8192), true, false, true, false)
			case "same-image":
				bus.Insert(rom.DOSWedge, false, true, false, true)
			}
			cpu = CPU{}
			game, exrom := cartridge.Game, cartridge.Exrom
			bus.Store(dosWedgeLatch, dosWedgeKill)
			Reset()
			if cartridge.Game != game || cartridge.Exrom != exrom || cartridge.killed {
				t.Fatal("wedge latch affected another cartridge")
			}
			if got := bus.Load(dosWedgeLatch); got != dosWedgeKill {
				t.Fatalf("ordinary I/O fallback = $%02X", got)
			}
		})
	}
}

// TestPLABankSwitchingThroughPort moves the banking the way real code
// does - by storing to the CPU's I/O port at $0001 - and reads back
// through the bus after each move. The PLA answers from a cached decode
// now, and a decode that is not re-derived when the banking moves does
// not fail loudly: it quietly keeps answering from the previous map. So
// this banks each window out and back in again, reading through it every
// time, rather than only checking the state it ends in.
func TestPLABankSwitchingThroughPort(t *testing.T) {
	saveMachine(t)
	bus.Remove()
	cpu = CPU{}

	const ramMarker = 0x5A
	for i := range ram {
		ram[i] = ramMarker
	}
	// $D800 reads three different ways depending on the banking, which
	// makes one address enough to tell I/O, character ROM and RAM apart.
	// The colour RAM's upper nibble floats high, see ioLoad.
	colorRAM[0] = 0x0A
	const colorRead = 0xFA

	cpu.store(0x0000, 0xFF) // drive all three lines, as IOINIT does

	for _, step := range []struct {
		port             uint8
		name             string
		a000, d800, e000 uint8
	}{
		{0x37, "BASIC, I/O, KERNAL", rom.Basic[0], colorRead, rom.Kernal[0]},
		{0x36, "LORAM low: BASIC out", ramMarker, colorRead, rom.Kernal[0]},
		{0x35, "HIRAM low: KERNAL out", ramMarker, colorRead, ramMarker},
		{0x34, "both low: I/O out too", ramMarker, ramMarker, ramMarker},
		{0x33, "CHAREN low: character ROM", rom.Basic[0], rom.Character[0x800], rom.Kernal[0]},
		{0x37, "everything banked back in", rom.Basic[0], colorRead, rom.Kernal[0]},
	} {
		cpu.store(0x0001, step.port)
		for _, probe := range []struct {
			addr uint16
			want uint8
		}{
			{0xA000, step.a000},
			{0xD800, step.d800},
			{0xE000, step.e000},
		} {
			if got := bus.Load(probe.addr); got != probe.want {
				t.Errorf("port=$%02X (%s): load($%04X) = $%02X, want $%02X",
					step.port, step.name, probe.addr, got, probe.want)
			}
		}
	}
}

// TestPLACartridgeEventsRebuildDecode covers the other half of the
// invalidation obligation. The decode is cached per Cartridge, so
// replacing the Cartridge discards it for free - but the paths that rewire
// /EXROM in place, the wedge latch and a hardware reset, have to discard it
// by hand. Every step reads through the map before the next one changes it,
// so a cache that outlived its wiring would answer here.
func TestPLACartridgeEventsRebuildDecode(t *testing.T) {
	saveMachine(t)

	const ramMarker = 0x77
	for i := range ram {
		ram[i] = ramMarker
	}
	cpu = CPU{}
	bus.Remove()
	if got := bus.Load(0x8000); got != ramMarker {
		t.Fatalf("empty port: load($8000) = $%02X, want RAM $%02X", got, ramMarker)
	}

	cartROM := make([]byte, 0x2000)
	for i := range cartROM {
		cartROM[i] = byte(i)
	}

	bus.Insert(cartROM, false, true, false, true) // 8K, ROM chip on /ROML
	if got := bus.Load(0x8000); got != cartROM[0] {
		t.Fatalf("after Insert: load($8000) = $%02X, want cartridge $%02X", got, cartROM[0])
	}

	bus.Remove()
	if got := bus.Load(0x8000); got != ramMarker {
		t.Fatalf("after Remove: load($8000) = $%02X, want RAM $%02X", got, ramMarker)
	}

	bus.Insert(cartROM, true, false, true, false) // MAX mode, ROM on /ROMH
	if got := bus.Load(0xE000); got != cartROM[0] {
		t.Fatalf("after MAX-mode Insert: load($E000) = $%02X, want cartridge $%02X", got, cartROM[0])
	}

	EnableDOSWedge()
	image := append([]byte(nil), cartridge.ROM...)
	if got := bus.Load(0x8000); got != image[0] {
		t.Fatalf("wedge mapped: load($8000) = $%02X, want $%02X", got, image[0])
	}
	bus.Store(dosWedgeLatch, dosWedgeKill) // releases /EXROM in place
	if got := bus.Load(0x8000); got != ramMarker {
		t.Fatalf("after wedge kill: load($8000) = $%02X, want RAM $%02X", got, ramMarker)
	}
	Reset() // asserts /EXROM again, also in place
	if got := bus.Load(0x8000); got != image[0] {
		t.Fatalf("after reset: load($8000) = $%02X, want $%02X", got, image[0])
	}
}

// TestPLABlockDecodeMatchesLines checks the cached decode against the
// chain of range tests it replaced, for every address in the CPU's space
// and every state of the five bits that feed it: the three bank-switching
// lines, and /GAME and /EXROM as the populated chip-selects qualify them.
//
// Every address, not a sample of block boundaries, because the claim the
// cache rests on is exactly that the decode does not change within a 4K
// block - so an address that disagrees with the old logic is the failure
// this is looking for, wherever in its block it sits.
func TestPLABlockDecodeMatchesLines(t *testing.T) {
	saveMachine(t)

	for _, wiring := range []struct {
		name                    string
		game, exrom, romh, roml bool
	}{
		{name: "empty port"},
		{name: "8K on /ROML", exrom: true, roml: true},
		{name: "8K, /ROML unpopulated", exrom: true},
		{name: "MAX mode on /ROMH", game: true, romh: true},
		{name: "MAX mode, /ROMH unpopulated", game: true},
		{name: "both lines asserted", game: true, exrom: true, romh: true, roml: true},
	} {
		cartridge = Cartridge{
			ROM:   make([]byte, 0x2000),
			Game:  wiring.game,
			Exrom: wiring.exrom,
			ROMH:  wiring.romh,
			ROML:  wiring.roml,
		}
		for sel := uint8(0); sel < 8; sel++ {
			cartridge.decodeBlocks(sel)
			loram, hiram, charen := sel&0x01 != 0, sel&0x02 != 0, sel&0x04 != 0
			for addr := 0; addr <= 0xFFFF; addr++ {
				// The decode plaLoad and plaStore used to perform inline,
				// spelled out here as the oracle it has to keep matching.
				want := blockRAM
				switch {
				case addr >= 0x8000 && addr <= 0x9FFF && cartridge.eightK() && cartridge.ROML && loram && hiram:
					want = blockCartROML
				case addr >= 0xA000 && addr <= 0xBFFF && loram && hiram:
					want = blockBasic
				case addr >= 0xD000 && addr <= 0xDFFF && (loram || hiram):
					want = blockCharROM
					if charen {
						want = blockIO
					}
				case addr >= 0xE000 && cartridge.ultimax() && cartridge.ROMH:
					want = blockCartROMH
				case addr >= 0xE000 && hiram:
					want = blockKernal
				}
				if got := cartridge.blocks[addr>>12]; got != want {
					t.Fatalf("%s, port lines $%X: $%04X decodes as %d, want %d",
						wiring.name, sel, addr, got, want)
				}
			}
		}
	}
}

// TestPLAZeroCartridgeMatchesNoSelector pins the freshness check from the
// other side: the zero value of Cartridge must never be mistaken for a
// decoded state, since Insert, Remove and a test assigning the struct all
// rely on that to discard the map along with the wiring it described.
func TestPLAZeroCartridgeMatchesNoSelector(t *testing.T) {
	var fresh Cartridge
	for sel := uint8(0); sel < 8; sel++ {
		if fresh.blockSel == sel+1 {
			t.Errorf("zero Cartridge matches selector $%X; a replaced cartridge would answer from the old map", sel)
		}
	}
}

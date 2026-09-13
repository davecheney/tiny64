package tiny64

import (
	"encoding/binary"
	"strings"
	"testing"
)

func testCRT(t *testing.T, hardwareType uint16, exrom, game bool, chips ...[]byte) []byte {
	t.Helper()

	crt := make([]byte, 0x40)
	copy(crt, crtHeaderMagic)
	binary.BigEndian.PutUint32(crt[0x10:0x14], 0x40)
	binary.BigEndian.PutUint16(crt[0x14:0x16], 0x0100)
	binary.BigEndian.PutUint16(crt[0x16:0x18], hardwareType)
	if !exrom {
		crt[0x18] = 1
	}
	if !game {
		crt[0x19] = 1
	}
	copy(crt[0x20:0x40], "TEST CART")
	return append(crt, chips...)
}

func testCRTChip(t *testing.T, bank, start uint16, data []byte) []byte {
	t.Helper()

	chip := make([]byte, 0x10+len(data))
	copy(chip, crtChipMagic)
	binary.BigEndian.PutUint32(chip[0x04:0x08], uint32(len(chip)))
	binary.BigEndian.PutUint16(chip[0x0A:0x0C], bank)
	binary.BigEndian.PutUint16(chip[0x0C:0x0E], start)
	binary.BigEndian.PutUint16(chip[0x0E:0x10], uint16(len(data)))
	copy(chip[0x10:], data)
	return chip
}

func TestParseCRTNormal8K(t *testing.T) {
	roml := make([]byte, 0x2000)
	roml[0], roml[len(roml)-1] = 0x80, 0x9F

	cart, err := ParseCRT(testCRT(t, crtHardwareNormal, true, false, testCRTChip(t, 0, 0x8000, roml)))
	if err != nil {
		t.Fatalf("ParseCRT failed: %v", err)
	}
	if cart.Name != "TEST CART" {
		t.Fatalf("cart.Name = %q, want TEST CART", cart.Name)
	}
	if !cart.Exrom || cart.Game {
		t.Fatalf("cart lines = game %v exrom %v, want game false exrom true", cart.Game, cart.Exrom)
	}
	if !cart.ROML || cart.ROMH {
		t.Fatalf("cart chips = roml %v romh %v, want only ROML", cart.ROML, cart.ROMH)
	}
	if got := cart.romlLoad(0x1FFF); got != 0x9F {
		t.Fatalf("cart.romlLoad(0x1fff) = %#02x, want 0x9f", got)
	}
}

func TestParseCRTNormal16KSingleChip(t *testing.T) {
	rom := make([]byte, 0x4000)
	rom[0], rom[0x2000], rom[len(rom)-1] = 0x80, 0xA0, 0xBF

	cart, err := ParseCRT(testCRT(t, crtHardwareNormal, true, true, testCRTChip(t, 0, 0x8000, rom)))
	if err != nil {
		t.Fatalf("ParseCRT failed: %v", err)
	}
	if !cart.ROML || !cart.ROMH {
		t.Fatalf("cart chips = roml %v romh %v, want both", cart.ROML, cart.ROMH)
	}
	if got := cart.romlLoad(0); got != 0x80 {
		t.Fatalf("cart.romlLoad(0) = %#02x, want 0x80", got)
	}
	if got := cart.romhLoad(0); got != 0xA0 {
		t.Fatalf("cart.romhLoad(0) = %#02x, want 0xa0", got)
	}
	if got := cart.romhLoad(0x1FFF); got != 0xBF {
		t.Fatalf("cart.romhLoad(0x1fff) = %#02x, want 0xbf", got)
	}
}

func TestParseCRTErrors(t *testing.T) {
	tests := []struct {
		name string
		crt  []byte
		want string
	}{
		{name: "short", crt: []byte("C64"), want: "shorter than 64-byte header"},
		{name: "magic", crt: make([]byte, 0x40), want: "has magic"},
		{name: "unsupported hardware", crt: testCRT(t, 5, true, true, testCRTChip(t, 0, 0x8000, []byte{1})), want: "unsupported CRT hardware type 5"},
		{name: "banked", crt: testCRT(t, crtHardwareNormal, true, true, testCRTChip(t, 1, 0x8000, []byte{1})), want: "unsupported banked CRT CHIP bank 1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseCRT(tt.crt)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseCRT error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPLACartridgeNormalMapping(t *testing.T) {
	saveMachine(t)

	roml := make([]byte, 0x2000)
	romh := make([]byte, 0x2000)
	roml[0], roml[0x1FFF] = 0x80, 0x9F
	romh[0], romh[0x1FFF] = 0xA0, 0xBF
	cartridge = Cartridge{
		ROMLData: roml,
		ROMHData: romh,
		Game:     true,
		Exrom:    true,
		ROML:     true,
		ROMH:     true,
	}

	if got := plaLoad(0x8000); got != 0x80 {
		t.Fatalf("plaLoad(0x8000) = %#02x, want 0x80", got)
	}
	if got := plaLoad(0x9FFF); got != 0x9F {
		t.Fatalf("plaLoad(0x9fff) = %#02x, want 0x9f", got)
	}
	if got := plaLoad(0xA000); got != 0xA0 {
		t.Fatalf("plaLoad(0xa000) = %#02x, want 0xa0", got)
	}
	if got := plaLoad(0xBFFF); got != 0xBF {
		t.Fatalf("plaLoad(0xbfff) = %#02x, want 0xbf", got)
	}
}

func TestPLACartridgeUltimaxMapping(t *testing.T) {
	saveMachine(t)

	roml := make([]byte, 0x2000)
	romh := make([]byte, 0x2000)
	roml[0] = 0x80
	romh[0] = 0xE0
	cartridge = Cartridge{
		ROMLData: roml,
		ROMHData: romh,
		Game:     true,
		Exrom:    false,
		ROML:     true,
		ROMH:     true,
	}

	if got := plaLoad(0x8000); got != 0x80 {
		t.Fatalf("plaLoad(0x8000) = %#02x, want 0x80", got)
	}
	if got := plaLoad(0xE000); got != 0xE0 {
		t.Fatalf("plaLoad(0xe000) = %#02x, want 0xe0", got)
	}
}

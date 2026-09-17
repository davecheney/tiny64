package tiny64

import (
	"testing"
)

// TestVICSpriteXRegisterRoundTrip pins the seam between the two ways a
// sprite's X is spelled. The CPU sees a low byte at $D000+2i and a ninth
// bit in $D010; the chip state keeps the whole nine bit number in spriteX.
// Writing either half must leave the other alone whichever order they
// arrive in, and both must read back as written.
func TestVICSpriteXRegisterRoundTrip(t *testing.T) {
	for i := uint16(0); i < 8; i++ {
		lowAddr := 0xD000 + i*2

		// Low byte first, then the MSB for this sprite alone.
		v := &VICII{}
		v.WriteRegister(lowAddr, 0xE0)
		v.WriteRegister(0xD010, uint8(1<<i))
		if got, want := v.spriteX[i], uint16(0x1E0); got != want {
			t.Errorf("sprite %d: low then MSB: spriteX = %#x, want %#x", i, got, want)
		}

		// The same two writes in the other order must land in the same
		// place: a write to $D010 must not disturb the low bits, and a
		// write to the low byte must not disturb bit 8.
		w := &VICII{}
		w.WriteRegister(0xD010, uint8(1<<i))
		w.WriteRegister(lowAddr, 0xE0)
		if got, want := w.spriteX[i], uint16(0x1E0); got != want {
			t.Errorf("sprite %d: MSB then low: spriteX = %#x, want %#x", i, got, want)
		}

		// Both halves read back as the CPU wrote them.
		if got, want := v.ReadRegister(lowAddr), uint8(0xE0); got != want {
			t.Errorf("sprite %d: $%04X = %#x, want %#x", i, lowAddr, got, want)
		}
		if got, want := v.ReadRegister(0xD010), uint8(1<<i); got != want {
			t.Errorf("sprite %d: $D010 = %#x, want %#x", i, got, want)
		}

		// Clearing the bit again drops X back below 256 without touching
		// the low byte, which is what a sprite moving left across dot 256
		// does.
		v.WriteRegister(0xD010, 0x00)
		if got, want := v.spriteX[i], uint16(0x0E0); got != want {
			t.Errorf("sprite %d: MSB cleared: spriteX = %#x, want %#x", i, got, want)
		}

		// Only this sprite moved.
		for j := uint16(0); j < 8; j++ {
			if j != i && w.spriteX[j] != 0 {
				t.Errorf("sprite %d: writing sprite %d left spriteX[%d] = %#x, want 0",
					i, i, j, w.spriteX[j])
			}
		}
	}
}

// TestVICSpritePropertiesDecodeRegisters drives every named sprite property
// from a register write, so that what each one decodes is checked against
// the address and bit it came from rather than against itself.
func TestVICSpritePropertiesDecodeRegisters(t *testing.T) {
	masks := []struct {
		name string
		addr uint16
		get  func(*VICII, uint8) bool
	}{
		{"Enabled", 0xD015, (*VICII).spriteEnabled},
		{"ExpandedY", 0xD017, (*VICII).spriteExpandedY},
		{"BehindGraphics", 0xD01B, (*VICII).spriteBehindGraphics},
		{"IsMulticolor", 0xD01C, (*VICII).spriteIsMulticolor},
		{"ExpandedX", 0xD01D, (*VICII).spriteExpandedX},
	}

	// A bit set for one sprite must read true for that sprite and false
	// for the other seven, in every mask register.
	for _, m := range masks {
		for i := uint8(0); i < 8; i++ {
			v := &VICII{}
			v.WriteRegister(m.addr, 1<<i)
			for j := uint8(0); j < 8; j++ {
				if got, want := m.get(v, j), j == i; got != want {
					t.Errorf("$%04X = %#x: %s(%d) = %v, want %v",
						m.addr, uint8(1<<i), m.name, j, got, want)
				}
			}
		}
	}

	// The per-sprite bytes, and the expansion bits' effect on size.
	for i := uint8(0); i < 8; i++ {
		v := &VICII{}
		v.WriteRegister(0xD000+uint16(i)*2, 0x50)
		v.WriteRegister(0xD001+uint16(i)*2, 0x40)
		// $D027+i keeps the whole byte; only the low nibble is a colour.
		v.WriteRegister(0xD027+uint16(i), 0xA7)

		if got, want := v.spriteXPos(i), uint16(0x50); got != want {
			t.Errorf("spriteXPos(%d) = %#x, want %#x", i, got, want)
		}
		if got, want := v.spriteYPos(i), uint8(0x40); got != want {
			t.Errorf("spriteYPos(%d) = %#x, want %#x", i, got, want)
		}
		if got, want := v.spriteColorOf(i), uint8(0x07); got != want {
			t.Errorf("spriteColorOf(%d) = %#x, want %#x", i, got, want)
		}
		if got, want := v.ReadRegister(0xD027+uint16(i)), uint8(0xA7); got != want {
			t.Errorf("$%04X = %#x, want the whole written byte %#x",
				0xD027+uint16(i), got, want)
		}

		// Unexpanded, then expanded in both directions.
		if got, want := v.spriteWidth(i), uint16(24); got != want {
			t.Errorf("spriteWidth(%d) = %d, want %d", i, got, want)
		}
		if got, want := v.spriteHeight(i), uint8(21); got != want {
			t.Errorf("spriteHeight(%d) = %d, want %d", i, got, want)
		}
		v.WriteRegister(0xD01D, 1<<i)
		v.WriteRegister(0xD017, 1<<i)
		if got, want := v.spriteWidth(i), uint16(48); got != want {
			t.Errorf("expanded spriteWidth(%d) = %d, want %d", i, got, want)
		}
		if got, want := v.spriteHeight(i), uint8(42); got != want {
			t.Errorf("expanded spriteHeight(%d) = %d, want %d", i, got, want)
		}

		// startDot is X plus the 24 dot offset to the display window,
		// wrapping at the end of the line rather than running off it.
		if got, want := v.spriteStartDot(i), uint16(0x50+24); got != want {
			t.Errorf("spriteStartDot(%d) = %d, want %d", i, got, want)
		}
		v.WriteRegister(0xD000+uint16(i)*2, 0xE0)
		v.WriteRegister(0xD010, 1<<i) // X = 480, so 480+24 wraps to 0
		if got, want := v.spriteStartDot(i), uint16(0); got != want {
			t.Errorf("wrapped spriteStartDot(%d) = %d, want %d", i, got, want)
		}
	}

	// spriteUnderDMA reads the latched display set, not $D015: a sprite
	// can be enabled without being under DMA, and stays under DMA for the
	// rest of its band after being disabled. See
	// TestVICSpriteDisableDoesNotRetractBand.
	v := &VICII{spriteDisplay: 0x81}
	for i := uint8(0); i < 8; i++ {
		if got, want := v.spriteUnderDMA(i), i == 0 || i == 7; got != want {
			t.Errorf("spriteUnderDMA(%d) = %v, want %v", i, got, want)
		}
	}
}

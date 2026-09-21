//go:build !vicmini

package tiny64

import "testing"

// The tests here sweep the VIC-II's graphics modes, so they only compile
// where there is more than one of them. A build made with -tags vicmini
// knows standard character mode alone; vic_modes_char_test.go is what
// holds that build to it.

func TestVICGraphicsModeColors(t *testing.T) {
	v := &VICII{
		background:         [4]uint8{1, 2, 3, 4},
		gdPending:          0x1B, // 00, 01, 10, 11
		videoBufferPending: 0x0D00 | 0xC1,
	}

	testMode := func(name string, control1, control2 uint8, want []byte) {
		t.Helper()
		v.control1, v.control2 = control1, control2
		v.loadGraphicsData()
		for i, color := range want {
			if got, _ := v.nextGraphicsColor(); got != color {
				t.Errorf("%s pixel %d = %d, want %d", name, i, got, color)
			}
		}
	}

	// The high Color RAM bit selects multicolor text. Clear it to prove
	// MCM alone still uses standard text for that character.
	v.videoBufferPending = 0x0500 | 0xC1
	testMode("standard text in MCM", 0, 0x10, []byte{1, 1, 1, 5, 5, 1, 5, 5})

	v.videoBufferPending = 0x0D00 | 0xC1
	testMode("multicolor text", 0, 0x10, []byte{1, 1, 2, 2, 3, 3, 5, 5})

	v.videoBufferPending = 0x00D6
	testMode("standard bitmap", 0x20, 0, []byte{6, 6, 6, 13, 13, 6, 13, 13})

	v.videoBufferPending = 0x0D06
	testMode("multicolor bitmap", 0x20, 0x10, []byte{1, 1, 0, 0, 6, 6, 13, 13})

	v.videoBufferPending = 0x0D00 | 0xC1
	testMode("ECM text", 0x40, 0, []byte{4, 4, 4, 13, 13, 4, 13, 13})
}

func TestVICGraphicsModeAddresses(t *testing.T) {
	saveMachine(t)
	cia.setVICBank(0)

	v := &VICII{RC: 3, VC: 12, memPointers: 0x08}
	v.videoMatrixColor[0] = 0x00C1

	ram[0x2063] = 0xA1
	v.control1, v.control2 = 0x20, 0
	v.cycleGAccess()
	if got := v.gdPending; got != 0xA1 {
		t.Fatalf("standard bitmap g-access = %#02x, want %#02x", got, 0xA1)
	}

	ram[0x200B] = 0xB2
	v.VC = 0
	v.VMLI = 0
	v.control1, v.control2 = 0x40, 0
	v.cycleGAccess()
	if got := v.gdPending; got != 0xB2 {
		t.Fatalf("ECM text g-access = %#02x, want %#02x", got, 0xB2)
	}
}

// TestGraphicsPaletteMatchesPerDotDecode holds refreshGraphicsPalette to
// the decode nextGraphicsColor used to perform per dot, across every
// input that decode could branch on: all eight graphics modes including
// the invalid ones, multicolor either way, and every value the latched
// g-access data can take.
//
// The palette is a pure function of those inputs, so an exhaustive
// comparison against the old code is both possible and cheap, and it is
// worth having: several of these arms - the invalid modes, ECM's choice
// of background register - are reached by no other test, and a wrong
// colour in one of them would show up as nothing more than an odd pixel
// in a mode nobody runs.
func TestGraphicsPaletteMatchesPerDotDecode(t *testing.T) {
	// perDotDecode is what nextGraphicsColor did for a sequencer value of
	// index, before the mode branching was lifted out of the dot path.
	perDotDecode := func(v *VICII, index uint8) (byte, bool) {
		if !v.multicolor {
			if index == 0 {
				if v.graphicsMode == modeECMText {
					return v.backgroundColor(uint8(v.videoBuffer>>6) & 0x03), false
				}
				if v.graphicsMode == modeStandardBitmap {
					return byte(v.videoBuffer) & 0x0F, false
				}
				if v.graphicsMode > modeECMText {
					return 0, false
				}
				return v.background[0], false
			}
			switch v.graphicsMode {
			case modeStandardText, modeMulticolorText, modeECMText:
				return byte(v.videoBuffer>>8) & 0x0F, true
			case modeStandardBitmap:
				return byte(v.videoBuffer>>4) & 0x0F, true
			default:
				return 0, false
			}
		}
		switch v.graphicsMode {
		case modeMulticolorText:
			switch index {
			case 0:
				return v.background[0], false
			case 1:
				return v.backgroundColor(1), false
			case 2:
				return v.backgroundColor(2), false
			default:
				return byte(v.videoBuffer>>8) & 0x07, true
			}
		case modeMulticolorBitmap:
			switch index {
			case 0:
				return v.background[0], false
			case 1:
				return byte(v.videoBuffer>>4) & 0x0F, true
			case 2:
				return byte(v.videoBuffer) & 0x0F, true
			default:
				return byte(v.videoBuffer>>8) & 0x0F, true
			}
		default:
			return 0, false
		}
	}

	v := &VICII{}
	// Distinct background colours, so swapping two of the four registers
	// cannot pass unnoticed.
	v.background = [4]uint8{0x01, 0x02, 0x03, 0x04} // $D021-$D024

	for mode := uint8(0); mode < 8; mode++ {
		for _, multicolor := range []bool{false, true} {
			// Only the first two sequencer values are reachable in the
			// standard modes; the multicolor modes shift out all four.
			indices := uint8(2)
			if multicolor {
				indices = 4
			}
			for data := 0; data < 1<<16; data++ {
				v.graphicsMode, v.multicolor = mode, multicolor
				v.videoBuffer = uint16(data)
				v.refreshGraphicsPalette()

				for index := uint8(0); index < indices; index++ {
					wantColor, wantForeground := perDotDecode(v, index)
					gotColor := v.gdColor[index]
					gotForeground := v.gdForeground&(1<<index) != 0
					if gotColor != wantColor || gotForeground != wantForeground {
						t.Fatalf("mode %d multicolor=%v data=%#04x index %d: palette says colour %#02x foreground=%v, per-dot decode says %#02x foreground=%v",
							mode, multicolor, data, index, gotColor, gotForeground, wantColor, wantForeground)
					}
				}
			}
		}
	}
}

// TestGraphicsDataExpansionMatchesPerDotShift holds expandGraphicsData
// to the shifting nextGraphicsColor used to perform per dot, across every
// g-access byte and both pixel widths.
//
// The mode test moved out of the dot path and into the reload, which is
// only sound if the widened register shifts out the same sequence of
// palette indices the 8-bit one did - including past the eighth dot,
// where the register has emptied and every further dot has to read as
// index 0. Ten dots, so two of them land there.
func TestGraphicsDataExpansionMatchesPerDotShift(t *testing.T) {
	// perDotShift is what nextGraphicsColor did for one dot, before the
	// pixel width was baked into the register.
	perDotShift := func(seq *uint8, half *bool, multicolor bool) uint8 {
		if !multicolor {
			index := *seq >> 7
			*seq <<= 1
			return index
		}
		index := *seq >> 6
		if *half {
			*seq <<= 2
		}
		*half = !*half
		return index
	}

	v := &VICII{}
	for _, multicolor := range []bool{false, true} {
		for data := 0; data < 1<<8; data++ {
			seq, half := uint8(data), false
			v.gdSequencer = expandGraphicsData(uint8(data), multicolor)
			for dot := range 10 {
				want := perDotShift(&seq, &half, multicolor)
				// gdColor is the identity here, so the colour the
				// sequencer reports is the index it shifted out.
				v.gdColor = [4]uint8{0, 1, 2, 3}
				got, _ := v.nextGraphicsColor()
				if got != want {
					t.Fatalf("data=%#02x multicolor=%v dot %d: widened register shifts out index %d, per-dot shift gives %d",
						data, multicolor, dot, got, want)
				}
			}
		}
	}
}

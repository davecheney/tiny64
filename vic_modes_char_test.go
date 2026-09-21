//go:build vicmini

package tiny64

import "testing"

// TestCharModeIgnoresTheOtherModeBits pins what vic_modes_char.go says it
// does: BMM, ECM and MCM are still writable, still read back, and still
// change nothing about the picture.
//
// It is worth a test rather than a comment because the failure is quiet.
// A build that kept half the decode would paint a text screen correctly
// and only diverge on a program that set one of these bits, which is the
// kind of thing that is discovered on hardware weeks later.
func TestCharModeIgnoresTheOtherModeBits(t *testing.T) {
	// $D011 bit 5 is BMM and bit 6 is ECM; $D016 bit 4 is MCM. Setting all
	// three at once is not a mode a real VIC-II has - it is one of the
	// invalid ones, which a full build paints black.
	for _, control1 := range []uint8{0x00, 0x20, 0x40, 0x60} {
		for _, control2 := range []uint8{0x00, 0x10} {
			v := &VICII{
				control1:           control1,
				control2:           control2,
				videoBufferPending: 0x0A41,
				gdPending:          0xA5,
				memPointers:        0x14,
				RC:                 3,
			}
			v.background[0] = 6

			// The g-access address is the text form whatever the bits say:
			// all eight bits of the character pointer, not the six ECM
			// leaves or the bitmap's VC.
			cb := (uint16(v.memPointers) >> 1) & 0x07
			want := cb<<11 | (v.videoBufferPending&0xFF)<<3 | uint16(v.RC)
			if got := v.gAccessAddress(cb); got != want {
				t.Errorf("control1=%#02x control2=%#02x: g-access address %#04x, want the text form %#04x",
					control1, control2, got, want)
			}

			// And the colours are the background and the video matrix's
			// nibble, never ECM's background pick or a multicolour pair.
			v.loadGraphicsData()
			if v.gdColor[0] != 6 {
				t.Errorf("control1=%#02x control2=%#02x: background colour %d, want 6",
					control1, control2, v.gdColor[0])
			}
			if v.gdColor[1] != 0x0A {
				t.Errorf("control1=%#02x control2=%#02x: foreground colour %#x, want the video matrix nibble $A",
					control1, control2, v.gdColor[1])
			}
			if v.gdForeground != 1<<1 {
				t.Errorf("control1=%#02x control2=%#02x: gdForeground %#02x, want %#02x",
					control1, control2, v.gdForeground, 1<<1)
			}

			// Standard resolution: eight one-bit pixels, two bits each in
			// the sequencer. $A5 is 0x4411 widened.
			if v.gdSequencer != 0x4411 {
				t.Errorf("control1=%#02x control2=%#02x: sequencer %#04x, want the hires expansion %#04x",
					control1, control2, v.gdSequencer, 0x4411)
			}
		}
	}
}

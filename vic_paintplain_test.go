package tiny64

import "testing"

// TestPaintPlainMatchesPaintWithNoSprites holds the sprite-free paint path
// to the general one. paintGraphicsPixelPlain drops the compositor
// outright rather than branching past it, which is only sound while
// spriteDisplay being zero means nothing can be covered.
//
// It sweeps the state the two share - the border flip-flop, the border
// colour, and the sequencer's contents - and requires the same pixel and
// the same sequencer left behind.
func TestPaintPlainMatchesPaintWithNoSprites(t *testing.T) {
	parkMachine(t)

	for _, border := range []bool{false, true} {
		for _, borderColor := range []uint8{0, 6, 14} {
			for _, seq := range []uint16{0x0000, 0x5555, 0x4000, 0xFFFF, 0x1234} {
				for dot := uint16(48); dot < 56; dot++ {
					general := &VICII{}
					general.Reset()
					plain := &VICII{}
					plain.Reset()

					for _, v := range []*VICII{general, plain} {
						v.dot = dot
						v.beamLine = 100
						v.mainBorder = border
						v.borderColor = borderColor
						v.gdSequencer = seq
						v.spriteDisplay = 0
						clear(v.spriteCoverage[:])
					}

					ClearFrameBuffer()
					general.paintGraphicsPixel(general.dot)
					want := frameBufferPixelRGBA(dot, 100)
					wantSeq := general.gdSequencer

					ClearFrameBuffer()
					plain.paintGraphicsPixelPlain(plain.dot)
					got := frameBufferPixelRGBA(dot, 100)

					if got != want || plain.gdSequencer != wantSeq {
						t.Fatalf("dot %d border=%v/%d seq=%#04x: general painted %v "+
							"leaving %#04x, plain painted %v leaving %#04x",
							dot, border, borderColor, seq, want, wantSeq,
							got, plain.gdSequencer)
					}
				}
			}
		}
	}
}

// TestZeroSpriteDisplayMeansNoCoverage is the fact the plain path rests on:
// with no sprite displayed, nothing is covered, so no dot can reach the
// compositor. rebuildSpriteCoverage is the only thing that fills the table.
func TestZeroSpriteDisplayMeansNoCoverage(t *testing.T) {
	parkMachine(t)
	v := &VICII{}
	v.Reset()

	// Fill it, so the check below cannot pass by never having been set.
	v.spriteDisplay = 0xFF
	for i := range 8 {
		v.spriteX[i] = uint16(24 + i*24)
	}
	v.rebuildSpriteCoverage()
	filled := false
	for _, c := range v.spriteCoverage {
		if c != 0 {
			filled = true
			break
		}
	}
	if !filled {
		t.Fatal("coverage stayed empty with all eight sprites displayed")
	}

	v.spriteDisplay = 0
	v.rebuildSpriteCoverage()
	for dot, c := range v.spriteCoverage {
		if c != 0 {
			t.Fatalf("dot %d still covered with spriteDisplay zero: %#02x", dot, c)
		}
	}
}

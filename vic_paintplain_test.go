package tiny64

import "testing"

// TestPlainGroupMatchesPaintWithNoSprites holds the sprite-free paint path
// to the general one. graphicsPixelPlain4 drops the compositor outright
// rather than branching past it, which is only sound while spriteDisplay
// being zero means nothing can be covered; and it hoists the border test
// and the sequencer's shifting out of its four dots, which is only sound
// while the border flip-flop cannot move under a group.
//
// So the group is compared against four graphicsPixel calls - a dot
// decided with all of that still in it - with the reload put where the
// group was told it falls. Both the four colours and the sequencer left
// behind have to match, since that sequencer is what the next group
// shifts.
//
// It sweeps the state the two share - the border flip-flop, the border
// colour, the sequencer's contents and what is pending for it - and a
// reload at each dot of the group as well as none at all, because where
// the reload falls decides how much of the shifting survives it replacing
// the register.
func TestPlainGroupMatchesPaintWithNoSprites(t *testing.T) {
	parkMachine(t)

	for _, border := range []bool{false, true} {
		for _, borderColor := range []uint8{0, 6, 14} {
			for _, seq := range []uint16{0x0000, 0x5555, 0x4000, 0xFFFF, 0x1234} {
				for _, pending := range []uint8{0x00, 0x5A, 0xFF} {
					for _, dot := range []uint16{48, 52} {
						for reloadOffset := uint16(0); reloadOffset <= DotsPerCycle/2; reloadOffset++ {
							group := &VICII{}
							group.Reset()
							general := &VICII{}
							general.Reset()

							for _, v := range []*VICII{group, general} {
								v.slot = dot / DotsPerCycle
								v.beamLine = 100
								v.mainBorder = border
								v.borderColor = borderColor
								v.gdSequencer = seq
								v.gdPending = pending
								v.videoBufferPending = 0x0700 | uint16(pending)
								v.spriteDisplay = 0
								clear(v.spriteCoverage[:])
							}

							var want [4]byte
							for i := range want {
								if uint16(i) == reloadOffset {
									general.loadGraphicsData()
								}
								want[i] = general.graphicsPixel(dot+uint16(i)) & 0x0F
							}

							c0, c1, c2, c3 := group.graphicsPixelPlain4(reloadOffset)
							got := [4]byte{c0 & 0x0F, c1 & 0x0F, c2 & 0x0F, c3 & 0x0F}

							if got != want || group.gdSequencer != general.gdSequencer {
								t.Fatalf("dot %d border=%v/%d seq=%#04x pending=%#02x reload=%d: "+
									"general painted %v leaving %#04x, group painted %v leaving %#04x",
									dot, border, borderColor, seq, pending, reloadOffset,
									want, general.gdSequencer, got, group.gdSequencer)
							}
						}
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

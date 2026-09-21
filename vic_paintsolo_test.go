//go:build !vicmini

package tiny64

import "testing"

// TestSoloGroupMatchesPaintWithOneSprite holds the solo-group paint path
// to the general one. graphicsPixelSolo4 settles the sprite, its row, its
// priority bit, the border flip-flop and whether its collision is still
// news before its first dot, all of which graphicsPixel decides per dot,
// so the two have to land on the same four colours from the same state.
//
// The colours are not the whole answer. The sequencer the group leaves
// behind is what the next group shifts, and $D01F and the interrupt latch
// are what a program reads to find out two things touched, so all four
// are compared.
//
// The sweep is over what the group hoists and what can move under it: the
// border flip-flop, the priority bit, whether the collision register
// already holds this sprite, where the reload falls, and a row that is
// transparent in front, behind and through the middle of the group -
// because a transparent dot is the one shape inside a solo group that
// takes a different branch from its neighbours.
func TestSoloGroupMatchesPaintWithOneSprite(t *testing.T) {
	parkMachine(t)

	const sprite = 3
	mask := spriteBit(sprite)

	rows := [][4]uint8{
		{spriteDotOwn, spriteDotOwn, spriteDotOwn, spriteDotOwn},
		{spriteDotNone, spriteDotNone, spriteDotNone, spriteDotNone},
		{spriteDotNone, spriteDotOwn, spriteDotMC0, spriteDotMC1},
		{spriteDotMC1, spriteDotNone, spriteDotOwn, spriteDotNone},
		{spriteDotMC0, spriteDotMC0, spriteDotNone, spriteDotOwn},
	}

	for _, border := range []bool{false, true} {
		for _, priority := range []bool{false, true} {
			for _, latched := range []uint8{0x00, mask} {
				for _, foreground := range []uint8{0, 1 << 1, 0xFF} {
					for _, seq := range []uint16{0x0000, 0x5555, 0x4000, 0xFFFF, 0x1234} {
						for _, row := range rows {
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
										v.borderColor = 14
										v.gdSequencer = seq
										v.gdPending = 0x5A
										v.videoBufferPending = 0x075A
										v.gdForeground = foreground
										v.gdColor = [4]byte{1, 2, 4, 5}

										v.spriteDisplay = mask
										v.spriteDataCollision = latched
										v.interruptEnable = 0x08
										v.spriteMC0, v.spriteMC1 = 9, 10
										v.spriteColor[sprite] = 11
										if priority {
											v.spritePriority = mask
										}

										// The group starts four dots into
										// the sprite, so the row is read
										// at an offset either way.
										v.spriteStart[sprite] = dot - 4
										copy(v.spritePixels[sprite][4:], row[:])
										clear(v.spriteCoverage[:])
										for k := range uint16(DotsPerCycle / 2) {
											v.spriteCoverage[dot+k] = mask
										}
									}

									var want [4]byte
									for k := range want {
										if uint16(k) == reloadOffset {
											general.loadGraphicsData()
										}
										want[k] = general.graphicsPixel(dot+uint16(k)) & 0x0F
									}

									c0, c1, c2, c3 := group.graphicsPixelSolo4(dot, reloadOffset, sprite)
									got := [4]byte{c0 & 0x0F, c1 & 0x0F, c2 & 0x0F, c3 & 0x0F}

									if got != want ||
										group.gdSequencer != general.gdSequencer ||
										group.spriteDataCollision != general.spriteDataCollision ||
										group.interruptStatus != general.interruptStatus ||
										group.IRQ != general.IRQ {
										t.Fatalf("dot %d border=%v priority=%v latched=%#02x fg=%#02x seq=%#04x row=%v reload=%d: "+
											"general painted %v leaving seq %#04x $D01F %#02x irq %#02x/%v, "+
											"group painted %v leaving seq %#04x $D01F %#02x irq %#02x/%v",
											dot, border, priority, latched, foreground, seq, row, reloadOffset,
											want, general.gdSequencer, general.spriteDataCollision, general.interruptStatus, general.IRQ,
											got, group.gdSequencer, group.spriteDataCollision, group.interruptStatus, group.IRQ)
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

// TestSoloGroupIsOneSpriteOverAllFourDots pins what spriteSoloGroup
// promises graphicsPixelSolo4, since the paint path indexes that sprite's
// row for all four dots without checking coverage again: a group it
// accepts has exactly one sprite over it, and that sprite covers every
// dot of it.
func TestSoloGroupIsOneSpriteOverAllFourDots(t *testing.T) {
	parkMachine(t)

	for _, tc := range []struct {
		name     string
		coverage [4]uint8
		want     uint8
		ok       bool
	}{
		{"uncovered", [4]uint8{0, 0, 0, 0}, 0, false},
		{"one sprite throughout", [4]uint8{0x08, 0x08, 0x08, 0x08}, 3, true},
		{"sprite 0 throughout", [4]uint8{0x01, 0x01, 0x01, 0x01}, 0, true},
		{"sprite 7 throughout", [4]uint8{0x80, 0x80, 0x80, 0x80}, 7, true},
		{"left edge", [4]uint8{0, 0, 0x08, 0x08}, 0, false},
		{"right edge", [4]uint8{0x08, 0x08, 0x08, 0}, 0, false},
		{"two sprites meeting", [4]uint8{0x08, 0x08, 0x10, 0x10}, 0, false},
		{"two sprites over one dot", [4]uint8{0x08, 0x18, 0x08, 0x08}, 0, false},
		{"two sprites throughout", [4]uint8{0x18, 0x18, 0x18, 0x18}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := &VICII{}
			v.Reset()
			const dot = 48
			for k, c := range tc.coverage {
				v.spriteCoverage[dot+k] = c
			}
			i, ok := v.spriteSoloGroup(dot)
			if ok != tc.ok || (ok && i != tc.want) {
				t.Fatalf("coverage %v: got sprite %d, %v; want %d, %v", tc.coverage, i, ok, tc.want, tc.ok)
			}
			// Every dot of an accepted group has to be inside that
			// sprite, because the paint path offsets its row by the
			// group's first dot and reads four bytes from there.
			if ok {
				for k := range uint16(DotsPerCycle / 2) {
					if v.spriteCoverage[dot+k]&spriteBit(i) == 0 {
						t.Fatalf("coverage %v: accepted sprite %d but dot +%d is not covered by it", tc.coverage, i, k)
					}
				}
			}
		})
	}
}

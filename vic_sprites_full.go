//go:build !vicmini

package tiny64

import "math/bits"

// This is the sprite seam for a full build: the VIC-II's eight sprites,
// their DMA fetch, the coverage table and the compositor that merges them
// with the graphics sequencer. A C64 running anything but a text screen
// needs all of it.
//
// It pairs with vic_sprites_stub.go, which answers the same calls for a
// build made with -tags vicmini. The split is by file rather than by a
// test inside each function because that is how every other seam here
// works - pixel_sink_*.go for the frame buffer, drive_virtual.go and
// drive_real.go for the 1541 - and because the dot path should not have
// to read a question it can never ask.

// graphicsPixelSolo4 decides the colours of all four dots of a Phi0
// half-phase that one sprite, and one alone, covers all of. i is that
// sprite, as spriteSoloGroup named it.
//
// Knowing the sprite before the first dot is what this is for. Its row,
// its mask, its priority bit and whether its data collision is still
// unreported are all settled once here; per dot there is a shift, a row
// byte, and the choice between three colours. The border flip-flop is
// the group's too, for the reason graphicsPixelPlain4 gives.
//
// The collision is the one piece of group state that can move under it,
// and only in one direction: a dot of this sprite over foreground
// graphics latches $D01F, and every later dot of the group then finds it
// already set. Tracking that in a local is the same answer the register
// would give, without loading it again. Nothing else can clear it inside
// a group - only the CPU reading $D01F does, and that lands between
// half-phases, never inside one.
//
// Unlike graphicsPixelPlain4 there is no shortcut behind a closed
// border. What the sequencer shifts out is still dead, but which dots
// this sprite paints is not: collisions are latched behind the border
// exactly as they are in front of it, so all four dots run.
//
// The four dots are a loop rather than four unrolled copies. What is per
// dot here is large enough that four of it is what made a whole-body
// graphicsPixel4 too big to be worth having; the loop keeps one copy of
// it and pays four predictable branches for the reload instead.
//
// TestSoloGroupMatchesPaintWithOneSprite holds it to four graphicsPixel
// calls.
func (v *VICII) graphicsPixelSolo4(dot, reloadOffset uint16, i uint8) (byte, byte, byte, byte) {
	mask := spriteBit(i)
	row := v.spritePixels[i][dot-v.spriteStart[i]:]
	behindGraphics := v.spritePriority&mask != 0
	collisionIsNews := mask&^v.spriteDataCollision != 0
	border := v.mainBorder

	var c [DotsPerCycle / 2]byte
	for k := range c {
		if uint16(k) == reloadOffset {
			v.loadGraphicsData()
		}
		graphicsColor, gdIndex := v.nextGraphicsColor()

		val := row[k]
		if val == spriteDotNone {
			if border {
				c[k] = v.borderColor
			} else {
				c[k] = graphicsColor
			}
			continue
		}

		isForeground := v.gdForeground&(1<<gdIndex) != 0
		if isForeground && collisionIsNews {
			v.raiseSpriteDataCollision(mask)
			collisionIsNews = false
		}

		switch {
		case border:
			c[k] = v.borderColor
		case !behindGraphics || !isForeground:
			c[k] = spriteDotColor(v, val, i)
		default:
			c[k] = graphicsColor
		}
	}
	return c[0], c[1], c[2], c[3]
}

// graphicsPixel decides one pixel's colour, through the border unit and
// the sprite compositor. It returns it rather than writing it: a dot's
// colour is per dot, but the write need not be, and dotclock4 collects
// four of these into one write.
//
// Section 3.9 of the VIC Article is explicit that the *main* border
// flip-flop alone decides whether border colour reaches the screen; the
// vertical border flip-flop is internal state that only feeds rules 4/5
// and 6. Gating output on the vertical flip-flop instead breaks every
// trick that opens a border, and - because it skips nextGraphicsColor -
// stalls the graphics sequencer for the whole of the upper and lower
// border, desynchronising the shift register against the g-accesses.
//
// nextGraphicsColor is therefore called unconditionally: the sequencer
// keeps shifting behind a closed border exactly as the hardware does, and
// only the colour that is written is replaced.
func (v *VICII) graphicsPixel(dot uint16) byte {
	graphicsColor, gdIndex := v.nextGraphicsColor()
	// Which sprites cover this dot, decided when the line's windows were
	// last settled rather than by asking all eight here. The mask keeps
	// the index provably inside the table; see spriteCoverage.
	display := v.spriteCoverage[dot&511]

	if display == 0 {
		if v.mainBorder {
			graphicsColor = v.borderColor
		}
		return graphicsColor
	}

	// Past the early-out, so this is only worked out for the dots a sprite
	// actually covers. See nextGraphicsColor.
	isForeground := v.gdForeground&(1<<gdIndex) != 0

	// More than one sprite over the one dot is the rare half, and none of
	// it is here: see graphicsPixelOverlap.
	if display&(display-1) != 0 {
		return v.graphicsPixelOverlap(dot, display, graphicsColor, isForeground)
	}

	// One sprite over the dot is the common case, and it does not need
	// any of the machinery the overlap path carries. Nothing can win the
	// pixel ahead of it, so there is no first-hit bookkeeping; nothing
	// can share the dot with it, so there is no sprite-sprite collision.
	// What is left is the dot's own value, one collision register and the
	// priority bit.
	i := uint8(bits.TrailingZeros8(display))
	val := v.spritePixels[i][dot-v.spriteStart[i]]
	if val == spriteDotNone {
		if v.mainBorder {
			graphicsColor = v.borderColor
		}
		return graphicsColor
	}

	mask := spriteBit(i)
	if isForeground && mask&^v.spriteDataCollision != 0 {
		v.raiseSpriteDataCollision(mask)
	}

	if v.mainBorder {
		return v.borderColor
	}
	if v.spritePriority&mask == 0 || !isForeground {
		return spriteDotColor(v, val, i)
	}
	return graphicsColor
}

// graphicsPixelOverlap decides a dot that more than one sprite covers.
// The first hit wins the pixel, and every sprite that painted the dot
// goes into the sprite-sprite collision register.
//
// It is out of line from graphicsPixel because it is the rare half, and
// the expensive one: a loop over the coverage mask, two collision
// registers, and first-hit bookkeeping that neither of the other two
// paths needs. Sprites have to be placed over one another to reach it at
// all. Kept here it costs the dots that need it a call and the dots that
// do not nothing, which is what leaves graphicsPixel small enough for a
// caller to take four of it.
func (v *VICII) graphicsPixelOverlap(dot uint16, display uint8, graphicsColor byte, isForeground bool) byte {
	d := dot
	priorityReg := v.spritePriority

	var (
		hitCount             int
		currentHitMask       uint8
		topSpriteColor       byte
		topSpritePriorityBit bool
	)

	// Lowest numbered sprite first, because the first hit wins the pixel:
	// walking the coverage mask from its low bit visits them in the same
	// order the range tests did.
	for remaining := display; remaining != 0; remaining &= remaining - 1 {
		i := uint8(bits.TrailingZeros8(remaining))
		mask := spriteBit(i)

		// The dot is inside this sprite's window - that is what coverage
		// means - so how far into it says which of the row's dots this
		// is, and decodeSpriteRow has already worked out what that dot
		// paints. Expansion is folded into the row, so this is a plain
		// offset either way.
		val := v.spritePixels[i][d-v.spriteStart[i]]
		if val == spriteDotNone {
			continue
		}

		color := spriteDotColor(v, val, i)

		hitCount++
		currentHitMask |= mask
		if hitCount == 1 {
			topSpriteColor = color
			topSpritePriorityBit = (priorityReg & mask) != 0 // $D01B priority
		}
	}

	// Both registers are latched until read, so once a sprite's bit is in
	// one every later dot leaves it exactly as it was. Testing before
	// storing keeps a read-modify-write off VICII state on every covered
	// dot, which is what the common case is - a collision is news once and
	// then background.
	if hitCount > 1 && currentHitMask&^v.spriteSpriteCollision != 0 {
		v.raiseSpriteSpriteCollision(currentHitMask)
	}

	if hitCount > 0 && isForeground && currentHitMask&^v.spriteDataCollision != 0 {
		v.raiseSpriteDataCollision(currentHitMask)
	}

	finalColor := graphicsColor
	// The right edge comparator is sampled on the character-cell boundary,
	// but a mid-line CSEL change can leave mainBorder open for that sample.
	// The boundary pixel is nevertheless part of the right border; masking it
	// here prevents the last graphics pixel from leaking into the border.
	if v.mainBorder {
		finalColor = v.borderColor
	} else if hitCount > 0 {
		if !topSpritePriorityBit || !isForeground {
			finalColor = topSpriteColor
		}
	}

	return finalColor
}

// spriteFreeGroup reports that no sprite covers any of a half-phase's
// four dots, which is what lets the group skip the compositor outright.
//
// The compositor's own early-out asks the same thing a dot at a time, and
// pays a call to do it. Asked here it is four loads folded together, or
// one when no sprite is displayed at all - and a sprite is 24 dots wide,
// so even on a line carrying eight of them most groups are not covered by
// any.
//
// Masking to a multiple of four keeps all four indices provably inside
// the table: every group starts on one, so the mask changes no answer
// that a real caller asks for.
func (v *VICII) spriteFreeGroup(dot uint16) bool {
	if v.spriteDisplay == 0 {
		return true
	}
	d := dot & (511 &^ 3)
	return v.spriteCoverage[d]|v.spriteCoverage[d+1]|
		v.spriteCoverage[d+2]|v.spriteCoverage[d+3] == 0
}

// spriteSoloGroup reports the one sprite covering every dot of a
// half-phase, when exactly one sprite covers all four.
//
// That is the shape a covered group almost always has. A sprite is 24
// dots wide against a group's four, so a group that meets a sprite at
// all is six times more likely to be inside it than on either edge of
// it, and sprites have to be placed over one another for a dot to have
// two. What the answer buys is everything about that sprite: which one
// it is, where its row sits, its priority bit and whether its collision
// is still news are all the group's, not the dot's.
//
// The four entries have to be equal as well as single-bit. A group that
// straddles an edge has some dots covered and some not, or two different
// sprites over its two halves, and neither can be decided once for the
// group - those take the per-dot path.
//
// Masking to a multiple of four keeps all four indices inside the table,
// as spriteFreeGroup does.
func (v *VICII) spriteSoloGroup(dot uint16) (uint8, bool) {
	d := dot & (511 &^ 3)
	c := v.spriteCoverage[d]
	if c == 0 || c&(c-1) != 0 {
		return 0, false
	}
	if v.spriteCoverage[d+1] != c || v.spriteCoverage[d+2] != c ||
		v.spriteCoverage[d+3] != c {
		return 0, false
	}
	return uint8(bits.TrailingZeros8(c)), true
}

// spriteBASlotMask gives, for each bus cycle of a line, the set of sprites
// whose BA window covers it. Sprite N's BA runs from slot 44+2N through
// 48+2N inclusive: the two cycles of pointer/data fetch it actually needs
// (slots 47+2N and 48+2N), preceded by the three cycles of lead time the
// VIC-II gives the CPU to retire any in-flight write cycles before the bus
// is taken away. Transcribed from the 6569 (PAL) cycle table in VICE's
// x64sc (viciisc/vicii-chip-model.c), rebased from its article cycle
// numbering onto slot (article cycle = slot + 11).
var spriteBASlotMask = [CyclesPerLine]uint8{
	44: 0x01, 45: 0x01, 46: 0x03, 47: 0x03, 48: 0x07, 49: 0x06,
	50: 0x0E, 51: 0x0C, 52: 0x1C, 53: 0x18, 54: 0x38, 55: 0x30,
	56: 0x70, 57: 0x60, 58: 0xE0, 59: 0xC0, 60: 0xC0, 61: 0x80,
	62: 0x80,
}

// spriteDMAStall reports whether any sprite whose BA window covers this
// slot is currently DMA active, and so is holding BA low. It asks that of
// all eight sprites at once, so it tests the display set as a whole byte
// rather than through spriteUnderDMA.
func (v *VICII) spriteDMAStall(slot uint16) bool {
	return spriteBASlotMask[slot]&v.spriteDisplay != 0
}

// latchSpriteDisplay advances each sprite's DMA state one raster line, and
// leaves in spriteDisplay the set of sprites that will be drawn on the
// *next* line, with spriteRow holding the row of each sprite's shape to
// fetch.
//
// The VIC-II runs this once per line, in the first phase of article cycle
// 55 (our slot 44). A sprite's DMA is a one-shot trigger: it turns on for
// the single line where the sprite's Y matches the low eight bits of
// RASTER, and from then on the chip walks the sprite's 21 rows off an
// internal counter (MCBASE) with no further reference to Y. Rewriting Y
// part way through cannot retract the rows already committed, which is
// exactly what a sprite multiplexer relies on: it reprograms a sprite for
// its next slot while the current one is still being drawn.
//
// Everything after this point - the pointer and data fetches, and the
// display window on the following line - runs off the state latched here.
func (v *VICII) latchSpriteDisplay() {
	enable := v.spriteEnable
	expandY := v.spriteExpandY
	line := uint8(v.rasterLine)

	for i := uint8(0); i < 8; i++ {
		mask := uint8(1) << i

		// Advance a sprite already under DMA to its next row. Y expansion
		// holds each row for two lines by only advancing on every second
		// one, tracked by the expansion flip flop.
		if v.spriteDisplay&mask != 0 {
			advance := true
			if expandY&mask != 0 {
				v.spriteExpFF ^= mask
				advance = v.spriteExpFF&mask == 0
			}
			if advance {
				if v.spriteRow[i] == 20 {
					v.spriteDisplay &^= mask
				} else {
					v.spriteRow[i]++
				}
			}
		}

		// A sprite whose DMA is off starts a new 21 row run on the line
		// its Y names. The comparison is against the low eight bits of
		// RASTER, so on PAL a sprite positioned above line 56 is triggered
		// a second time when the raster passes 256 + Y.
		if v.spriteDisplay&mask == 0 && enable&mask != 0 &&
			v.spriteYPos(i) == line {
			v.spriteDisplay |= mask
			v.spriteRow[i] = 0
			v.spriteExpFF |= mask
		}
	}
	v.rebuildSpriteCoverage()
}

// rebuildSpriteCoverage works out which dots each displayed sprite covers,
// so that the dot path can ask one table lookup instead of running eight
// range tests.
//
// The tests it replaces are overwhelmingly negative. A sprite is 24 dots
// wide on a 504 dot line, so on a line carrying all eight the compositor
// used to run 3,240 range tests to find at most 192 covered dots; measured
// over a frame of uncle-agnus-mcfungus, 671,895 iterations found 39,816
// covered dots, and 94% of the work was discarded. Each discarded test
// still loaded the sprite's X from two register arrays, reassembled its
// ninth bit from $D010, and recomputed (24 + x) % DotsPerLine - none of
// which can change between one dot and the next.
//
// Building the table costs at most 384 writes, one per covered dot, and
// the window is clamped at the end of the line rather than wrapped onto
// the next, which is what the range test it replaces did.
//
// Being derived state, it has to be rebuilt wherever its inputs change,
// and the four inputs reach only three places between them:
//
//   - the sprite X registers and their ninth bits in $D010, written in
//     exactly one place, WriteRegister's reg <= 0x10 case;
//   - $D01D's expansion bits, likewise, in the reg < regBorderColor case;
//   - spriteDisplay, changed only by latchSpriteDisplay, which ends by
//     calling this, and cleared by Reset, which clears the table too.
//
// Four inputs collapsing to three sites is a property of how the register
// arrays are written today - each has a single choke point - rather than
// anything this arranges, so it is worth saying out loud: a second writer
// of any of them would need a fourth call, and would not announce itself.
// TestSpriteCoverageStaysConsistentAcrossAFrame is what would catch that,
// by rebuilding after every bus cycle of a live frame and comparing.
//
// Rebuilding on the write rather than latching once a line is what keeps a
// mid-line write to a sprite's position taking effect on the next dot, as
// it did when the registers were read per dot.
func (v *VICII) rebuildSpriteCoverage() {
	clear(v.spriteCoverage[:])
	if v.spriteDisplay == 0 {
		return
	}
	for i := uint8(0); i < 8; i++ {
		if !v.spriteUnderDMA(i) {
			continue
		}
		mask := spriteBit(i)

		// spriteStartDot places the sprite by its leftmost dot, 24 dots
		// left of the display window's first column, and carries the
		// wrap; spriteWidth doubles it for $D01D.
		start := v.spriteStartDot(i)
		v.spriteStart[i] = start

		end := min(start+v.spriteWidth(i), DotsPerLine)
		for dot := start; dot < end; dot++ {
			v.spriteCoverage[dot] |= mask
		}
	}
}

// spriteHiresDots and spriteMulticolorDots turn one byte of a sprite's
// shape into the dots it paints, in the encoding decodeSpriteRow uses.
// Both are eight dots wide, because a byte is eight hires pixels or four
// multicolour pixels and a multicolour pixel is two dots.
//
// Tables rather than shifting per dot: a row is three bytes, so decoding
// it is three copies of eight bytes instead of twenty-four bit
// extractions.
var (
	spriteHiresDots      = buildSpriteHiresDots()
	spriteMulticolorDots = buildSpriteMulticolorDots()
)

func buildSpriteHiresDots() [256][8]uint8 {
	var table [256][8]uint8
	for b := range table {
		for i := range table[b] {
			if b&(0x80>>i) != 0 {
				table[b][i] = spriteDotOwn
			}
		}
	}
	return table
}

func buildSpriteMulticolorDots() [256][8]uint8 {
	var table [256][8]uint8
	for b := range table {
		for pair := range 4 {
			// Pairs run high bits first, and each paints two dots.
			val := uint8(b>>(6-2*pair)) & 0x03
			table[b][pair*2] = val
			table[b][pair*2+1] = val
		}
	}
	return table
}

// The dot values decodeSpriteRow emits. They are the multicolour bit pair
// verbatim, which is what lets the multicolour table be the pairs
// themselves: 00 is transparent, 01 is $D025, 10 is the sprite's own
// colour and 11 is $D026. A hires pixel is transparent or the sprite's
// own colour, so it uses the same two values.
const (
	spriteDotNone = 0
	spriteDotMC0  = 1
	spriteDotOwn  = 2
	spriteDotMC1  = 3
)

// spriteDotColor turns a decoded dot value into the colour it paints.
//
// The value names one of the four colour registers rather than holding a
// colour, which is what lets decodeSpriteRow run once a line while a
// mid-line write to $D025, $D026 or $D027-$D02E still lands on the dots
// after it.
func spriteDotColor(v *VICII, val, i uint8) byte {
	switch val {
	case spriteDotMC0:
		return v.spriteMC0 & 0x0F
	case spriteDotOwn:
		return v.spriteColor[i] & 0x0F
	default:
		return v.spriteMC1 & 0x0F
	}
}

// decodeSpriteRow works out the dots sprite i paints, once for the line,
// so that graphicsPixel can index the answer instead of deriving it
// for every dot the sprite covers.
//
// What it does not bake in is colour. The values are which of the four
// colour registers a dot takes, not the colour itself, so a mid-line
// write to $D025, $D026 or $D027-$D02E still lands - those registers are
// read where the dot is painted. Only the two registers that change the
// shape of the row rather than its colours have to invalidate this:
// $D01C, which decides whether a byte is eight pixels or four, and
// $D01D, which doubles their width.
func (v *VICII) decodeSpriteRow(i uint8) {
	mask := spriteBit(i)
	table := &spriteHiresDots
	if v.spriteMulticolor&mask != 0 {
		table = &spriteMulticolorDots
	}
	row := v.spritePixels[i][:]
	if v.spriteExpandX&mask == 0 {
		for b, shape := range v.spriteShape[i] {
			copy(row[b*8:b*8+8], table[shape][:])
		}
		return
	}
	// Expanded, so every dot is painted twice and the row is 48 long.
	for b, shape := range v.spriteShape[i] {
		dots := &table[shape]
		for j, val := range dots {
			row[b*16+j*2] = val
			row[b*16+j*2+1] = val
		}
	}
}

// decodeSpriteRows redecodes every sprite under DMA, for the two register
// writes that change how a row is laid out without changing the bytes it
// came from. Redecoding a whole row mid-line is right: the dots already
// painted are in the frame buffer, and only later lookups see the change.
func (v *VICII) decodeSpriteRows() {
	for i := uint8(0); i < 8; i++ {
		if v.spriteUnderDMA(i) {
			v.decodeSpriteRow(i)
		}
	}
}

// spriteUnderDMA reports whether sprite i is in the set latched for the
// next raster line, which is what decides both whether it is drawn and
// whether it steals bus cycles.
func (v *VICII) spriteUnderDMA(i uint8) bool { return v.spriteDisplay&spriteBit(i) != 0 }

// latchSpriteShape performs sprite i's pointer and data fetches, the
// p-access and three s-accesses the VIC-II makes in article cycles 58+2i
// and 59+2i. The three bytes are one row of the sprite, chosen by the row
// counter the chip derives from how far into its Y band the sprite is.
func (v *VICII) latchSpriteShape(i uint8) {
	if !v.spriteUnderDMA(i) {
		return
	}
	screenBase := (uint16(v.memPointers) >> 4) & 0x0F << 10
	ptr := plaVICSpriteLoad(screenBase + 0x03F8 + uint16(i))
	addr := uint16(ptr)*64 + uint16(v.spriteRow[i])*3
	v.spriteShape[i][0] = plaVICSpriteLoad(addr)
	v.spriteShape[i][1] = plaVICSpriteLoad(addr + 1)
	v.spriteShape[i][2] = plaVICSpriteLoad(addr + 2)
	v.decodeSpriteRow(i)
}

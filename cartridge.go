package tiny64

// Cartridge holds the state of whatever is plugged into the expansion
// port. The zero value matches an empty port: /GAME and /EXROM float high
// (unasserted), so the PLA falls back to normal CPU-port-driven banking.
type Cartridge struct {
	ROM   []byte
	Game  bool // /GAME line: true = pulled low (asserted)
	Exrom bool // /EXROM line: true = pulled low (asserted)
	ROMH  bool // /ROMH chip-select populated with a physical ROM chip
	ROML  bool // /ROML chip-select populated with a physical ROM chip

	dosWedge bool
	killed   bool

	// blocks caches the PLA's decode of the CPU's view of memory:
	// blocks[addr>>12] is the kind of memory selected at addr while the
	// bank-switching lines stand as blockSel records. Only the state the
	// machine is in is kept, not all thirty-two it could be in: the lines
	// move rarely next to the ~14,000 accesses a PAL frame makes through
	// them, so re-deriving sixteen entries when they do costs less than
	// carrying a decode of every state around in RAM.
	//
	// blockSel holds those lines biased by one, so that zero - the value
	// a freshly replaced Cartridge carries - matches no state they can be
	// in. That is why the decode lives here rather than beside the PLA:
	// the cartridge lines are the only input to it the access path does
	// not read live, and replacing the Cartridge value (Insert, Remove, or
	// a test assigning the struct) discards the decode along with the
	// wiring it described, for one instruction on the access path and no
	// invalidation call at all. Mutating a line in place instead (reset,
	// writeWedgeLatch) has to call invalidateBlocks by hand; a stale
	// decode here is silent corruption, not a crash.
	blockSel uint8
	blocks   [16]uint8
}

// invalidateBlocks discards the cached decode, so the next CPU access
// derives it again. Every path that changes /GAME, /EXROM or a populated
// chip-select without replacing the whole Cartridge must call this.
func (c *Cartridge) invalidateBlocks() {
	c.blockSel = 0
}

// decodeBlocks works out which chip the PLA selects in each 4K block of
// the CPU's address space, for the bank-switching lines sel describes,
// and records sel as the state the answer belongs to. This is the same
// decode plaLoad and plaStore used to perform inline on every access.
//
// Sixteen entries describe the map exactly, because every boundary in it
// - $8000, $A000, $C000, $D000, $E000 - is 4K aligned.
func (c *Cartridge) decodeBlocks(sel uint8) {
	loram := sel&0x01 != 0
	hiram := sel&0x02 != 0
	charen := sel&0x04 != 0

	for i := range c.blocks {
		c.blocks[i] = blockRAM
	}
	if loram && hiram {
		// The PLA only asserts /ROML when both LORAM and HIRAM are high,
		// which is why software banks a cartridge out by clearing LORAM
		// rather than by anything the cartridge itself provides.
		if c.eightK() && c.ROML {
			c.blocks[0x8], c.blocks[0x9] = blockCartROML, blockCartROML
		}
		c.blocks[0xA], c.blocks[0xB] = blockBasic, blockBasic
	}
	if loram || hiram {
		kind := blockCharROM
		if charen {
			kind = blockIO
		}
		c.blocks[0xD] = kind
	}
	switch {
	case c.ultimax() && c.ROMH:
		// A cartridge wired for MAX mode overrides the KERNAL entirely,
		// regardless of hiram.
		c.blocks[0xE], c.blocks[0xF] = blockCartROMH, blockCartROMH
	case hiram:
		c.blocks[0xE], c.blocks[0xF] = blockKernal, blockKernal
	}
	c.blockSel = sel + 1
}

var cartridge Cartridge

// The DOS wedge's custom PCB decodes IO2 for a ROM bootstrap aperture and
// a write-only latch. Bit 0 asserts /EXROM; bit 7 releases it and locks out
// both ROM and I/O until RESET. This is not an FC3 register implementation.
const (
	dosWedgeIO     = 0xDF00
	dosWedgeLatch  = 0xDFFF
	dosWedgeMap    = 0x01
	dosWedgeKill   = 0x80
	dosWedgeIOSize = 0x100
	dosWedgeIOBank = dosWedgeSize - dosWedgeIOSize
)

func (c *Cartridge) reset() {
	if c.dosWedge {
		c.killed = false
		c.Exrom = true
		c.invalidateBlocks()
	}
}

func (c *Cartridge) wedgeIO() bool {
	return c.dosWedge && !c.killed
}

func (c *Cartridge) writeWedgeLatch(val byte) {
	c.killed = val&dosWedgeKill != 0
	c.Exrom = !c.killed && val&dosWedgeMap != 0
	c.invalidateBlocks()
}

// ultimax reports whether the cartridge's /GAME and /EXROM lines are wired
// the way the DiSTestMAX build instructions describe: /GAME low, /EXROM
// high or floating. This overrides the CPU's LORAM/HIRAM/CHAREN banking
// entirely on real hardware, but for now tiny64 only special-cases the
// $E000-$FFFF KERNAL area (see plaLoad). The other two things MAX mode
// does - mapping /ROML at $8000-$9FFF and disabling RAM above $1000 - are
// not modeled, because no cartridge tiny64 runs in MAX mode populates
// /ROML or reads RAM up there.
func (c *Cartridge) ultimax() bool {
	return c.Game && !c.Exrom
}

// eightK reports whether the cartridge is wired the way an ordinary 8K
// cartridge is: /EXROM pulled low (asserted), /GAME left floating. The PLA
// then maps the /ROML image at $8000-$9FFF whenever the CPU is driving
// LORAM and HIRAM high, which is the state IOINIT leaves the CPU port in -
// unless the cartridge's own hardware releases /EXROM.
func (c *Cartridge) eightK() bool {
	return c.Exrom && !c.Game
}

// Insert plugs a cartridge into the expansion port. rom is the raw ROM
// image; game/exrom wire the /GAME and /EXROM lines (true = pulled low,
// asserted); romh/roml indicate which of the cartridge's chip-selects are
// actually populated with a ROM chip on the PCB.
func (b *Bus) Insert(rom []byte, game, exrom, romh, roml bool) {
	cartridge = Cartridge{ROM: rom, Game: game, Exrom: exrom, ROMH: romh, ROML: roml}
}

// Remove unplugs the cartridge, restoring /GAME and /EXROM to their
// floating (no cartridge present) state.
func (b *Bus) Remove() {
	cartridge = Cartridge{}
}

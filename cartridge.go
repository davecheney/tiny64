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

	// pages caches the PLA's page decode for the CPU's view of memory:
	// pages[bankSelect][addr>>8] is the kind of memory selected at addr
	// while the CPU port drives that combination of LORAM/HIRAM/CHAREN.
	// It lives here, rather than beside the PLA, because the cartridge
	// lines are the only input to it that is not read live on every
	// access: replacing the Cartridge value - Insert, Remove, or a test
	// assigning the struct - zeroes the cache along with the wiring it
	// described, and a zeroed entry is pageInvalid, which forces a
	// rebuild. Mutating a line in place instead (reset, writeWedgeLatch)
	// has to call invalidatePages by hand; a stale table here is silent
	// corruption, not a crash.
	pages [8][256]uint8
}

// invalidatePages discards the cached page decode, so the next CPU access
// rebuilds it. Every path that changes /GAME, /EXROM or a populated
// chip-select without replacing the whole Cartridge must call this.
func (c *Cartridge) invalidatePages() {
	c.pages = [8][256]uint8{}
}

// decodePages works out, for every page of the CPU's address space and
// every state of its three bank-switching lines, which chip the PLA
// selects. This is the same decode plaLoad and plaStore used to perform
// inline on each access; doing it once per cartridge change instead is
// only sound because nothing else it reads can change without the
// Cartridge changing too.
func (c *Cartridge) decodePages() {
	// Whether the cartridge is wired to answer at all, and with which of
	// its two chip-selects. Both are fixed for a given Cartridge value.
	roml := c.eightK() && c.ROML
	romh := c.ultimax() && c.ROMH
	for sel := range c.pages {
		loram := sel&0x01 != 0
		hiram := sel&0x02 != 0
		charen := sel&0x04 != 0
		for page := range c.pages[sel] {
			addr := uint16(page) << 8
			kind := pageRAM
			switch {
			case addr >= 0x8000 && addr <= 0x9FFF && roml && loram && hiram:
				kind = pageCartROML
			case addr >= 0xA000 && addr <= 0xBFFF && loram && hiram:
				kind = pageBasic
			case addr >= 0xD000 && addr <= 0xDFFF && (loram || hiram):
				kind = pageCharROM
				if charen {
					kind = pageIO
				}
			case addr >= 0xE000 && romh:
				kind = pageCartROMH
			case addr >= 0xE000 && hiram:
				kind = pageKernal
			}
			c.pages[sel][page] = kind
		}
	}
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
		c.invalidatePages()
	}
}

func (c *Cartridge) wedgeIO() bool {
	return c.dosWedge && !c.killed
}

func (c *Cartridge) writeWedgeLatch(val byte) {
	c.killed = val&dosWedgeKill != 0
	c.Exrom = !c.killed && val&dosWedgeMap != 0
	c.invalidatePages()
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

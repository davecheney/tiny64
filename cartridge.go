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
}

var cartridge Cartridge

// ultimax reports whether the cartridge's /GAME and /EXROM lines are wired
// the way the DiSTestMAX build instructions describe: /GAME low, /EXROM
// high or floating. This overrides the CPU's LORAM/HIRAM/CHAREN banking
// entirely on real hardware, but for now tiny64 only special-cases the
// $E000-$FFFF KERNAL area (see plaLoad).
func (c *Cartridge) ultimax() bool {
	return c.Game && !c.Exrom
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

package tiny64

type VICII struct {
	Dot        int16 // 0 to 503 (PAL) or 0 to 520 (NTSC)
	RasterLine int16 // 0 to 311 (PAL) or 0 to 262 (NTSC)

	// Signals driven by the VIC-II and sensed by the CPU
	BA  bool // Bus Available (true = high/free, false = low/stalled)
	AEC bool // Address Enable Control (true = CPU owns Phi2, false = VIC owns Phi2)

	// VIC-II Internal Registers
	registers [47]uint8 // d000 to d02e
}

var vic VICII

func (v *VICII) StepDot() {
	// 1. Advance the video beam by exactly 1 pixel/dot
	v.Dot++
	if v.Dot >= 63*8 { // Assuming PAL: 63 cycles * 8 dots = 504 dots
		v.Dot = 0
		v.RasterLine++
		if v.RasterLine >= 312 { // PAL has 312 total raster lines
			v.RasterLine = 0
		}
	}

	// 2. Determine Clock Phase boundaries.
	// Every 8 dots represents 1 full CPU cycle (Phi1 + Phi2).
	dotInCycle := v.Dot % 8

	switch dotInCycle {
	case 0:
		v.TickPhi1()
	case 4:
		// 3. Evaluate BA line timing right before Phi2 hits.
		// If this is a Bad Line and we are in the character fetching window:
		if v.isBadLine() && v.Dot >= 120 && v.Dot < 440 {
			v.BA = false
		} else {
			v.BA = true
		}

		// AEC mirrors BA with a delay, or is directly controlled here
		v.AEC = v.BA

		// 4. Trigger the CPU execution phase
		cpu.TickPhi2()
	}
}

func (v *VICII) TickPhi1() {
	// Phi1 belongs strictly to the VIC-II for processing graphics text,
	// sprites, backgrounds, and updating internal memory signals.
	if !v.BA {
		// If BA is low, VIC-II performs its text/sprite memory fetches here
		// e.g., vic_fetch_matrix_data()
	}
}

func (v *VICII) isBadLine() bool {
	// Simplified Bad Line check logic
	// Real logic checks if RasterLine is within the display window
	// and matches the lower 3 bits of ControlReg1 (YSCROLL)
	return v.RasterLine >= 51 && v.RasterLine <= 251 && (v.RasterLine&7) == 0
}

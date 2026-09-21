package tiny64

import "testing"

// TestPhi0LowDisplayMatchesPhi0Low checks the specialised phase against the
// general one across the run it claims to cover. phi0lowDisplay works by
// knowing the answer to every slot test in phi0low for slots
// displayFirstSlot through displaySlotAfter-1; if a new one-off cycle is
// ever added inside that range, or one of the three ranges is narrowed to
// no longer cover it whole, the two will disagree here.
func TestPhi0LowDisplayMatchesPhi0Low(t *testing.T) {
	parkMachine(t)

	// Walk the states the run is sensitive to rather than one arbitrary
	// one: the Bad Line window's edges, a line inside it at every YSCROLL
	// alignment, and DEN both ways.
	rasters := []uint16{0, 1, badLineRasterStart - 1, badLineRasterStart,
		badLineRasterStart + 3, 100, 101, badLineRasterEnd, badLineRasterEnd + 1}

	for _, raster := range rasters {
		for yscroll := uint8(0); yscroll < 8; yscroll++ {
			for _, den := range []uint8{0, 0x10} {
				for _, allow := range []bool{false, true} {
					for slot := uint16(displayFirstSlot); slot < displaySlotAfter; slot++ {
						general := &VICII{}
						general.Reset()
						special := &VICII{}
						special.Reset()

						for _, v := range []*VICII{general, special} {
							v.rasterLine = raster
							v.control1 = den | yscroll
							v.allowBadLine = allow
							v.slot = slot
							v.syncLineVisibility()
						}

						general.phi0low(slot)
						special.phi0lowDisplay()

						if general.badLine != special.badLine ||
							general.BA != special.BA ||
							general.baLowCycles != special.baLowCycles ||
							general.idle != special.idle ||
							general.denLatch != special.denLatch ||
							general.VC != special.VC ||
							general.VMLI != special.VMLI ||
							general.gdPending != special.gdPending {
							t.Fatalf("slot %d raster %d yscroll %d den %#02x allow %v: "+
								"general badLine=%v BA=%v baLow=%d idle=%v den=%v VC=%d VMLI=%d gd=%#02x, "+
								"special badLine=%v BA=%v baLow=%d idle=%v den=%v VC=%d VMLI=%d gd=%#02x",
								slot, raster, yscroll, den, allow,
								general.badLine, general.BA, general.baLowCycles,
								general.idle, general.denLatch, general.VC,
								general.VMLI, general.gdPending,
								special.badLine, special.BA, special.baLowCycles,
								special.idle, special.denLatch, special.VC,
								special.VMLI, special.gdPending)
						}
					}
				}
			}
		}
	}
}

// TestPhi0HighDisplayMatchesPhi0High holds the Phi0 high half to the same
// standard: across the run, the c-access range covers every slot, so only
// the Bad Line question should be left.
func TestPhi0HighDisplayMatchesPhi0High(t *testing.T) {
	parkMachine(t)
	for _, badLine := range []bool{false, true} {
		for slot := uint16(displayFirstSlot); slot < displaySlotAfter; slot++ {
			general := &VICII{}
			general.Reset()
			special := &VICII{}
			special.Reset()
			for _, v := range []*VICII{general, special} {
				v.badLine = badLine
				v.slot = slot
			}
			general.phi0high(slot)
			special.phi0highDisplay()
			if general.VC != special.VC || general.VMLI != special.VMLI ||
				general.videoBuffer != special.videoBuffer {
				t.Fatalf("slot %d badLine=%v: general VC=%d VMLI=%d buf=%#04x, "+
					"special VC=%d VMLI=%d buf=%#04x", slot, badLine,
					general.VC, general.VMLI, general.videoBuffer,
					special.VC, special.VMLI, special.videoBuffer)
			}
		}
	}
}

// TestDisplayRunCarriesNoOneOffCycles states the same thing from the
// constants: no cycle phi0low singles out may lie inside the run, and the
// three ranges must cover it whole. This is what licenses phi0lowDisplay
// dropping those tests, and it fails from the constants alone rather than
// needing the run walked.
func TestDisplayRunCarriesNoOneOffCycles(t *testing.T) {
	for _, oneOff := range []uint16{1, 2, 3, 44, 47, 52, vincSlot, vincSlot + 1} {
		if oneOff >= displayFirstSlot && oneOff < displaySlotAfter {
			t.Errorf("slot %d is a one-off cycle inside the display run [%d,%d)",
				oneOff, displayFirstSlot, displaySlotAfter)
		}
	}
	// Sprite shape latching starts at 47 and takes every odd slot after.
	if displaySlotAfter > 47 {
		t.Errorf("the display run reaches slot 47, where sprite shape latching starts")
	}
	// BA on a Bad Line covers 1..43, the c-access 4..43, the g-access 5..44.
	if displayFirstSlot < 5 || displaySlotAfter > 44 {
		t.Errorf("the display run [%d,%d) is not covered whole by phi0low's ranges",
			displayFirstSlot, displaySlotAfter)
	}
}

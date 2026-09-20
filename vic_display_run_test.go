package tiny64

import "testing"

// The display-run half-phases drop every slot test phi0low and phi0high
// make, on the grounds that each has a fixed answer across slots
// displayFirstSlot..displaySlotAfter-1. That is only true while phi0low
// and phi0high keep the ranges they have now, so hold it directly: run
// each slot of the run both ways from the same state and require the
// machine to land in the same place.
//
// The states cover what the half-phases actually branch on - the Bad Line
// window and its edges, YSCROLL, DEN, the idle/display flag and the
// video counters - rather than sweeping the whole register file, since a
// slot test can only be masked by state that reaches one of those.
func displayRunStates(t *testing.T) []VICII {
	saveMachine(t)

	// The c- and g-accesses read through the PLA and the colour RAM, and
	// write what they read into the row buffer. Against zeroed memory that
	// write stores a zero over a zero and a dropped access looks like an
	// equal one, so seed both with values that make an access visible.
	for addr := range ram {
		ram[addr] = byte(addr*7 + 1)
	}
	for addr := range colorRAM {
		colorRAM[addr] = byte(addr*5+3) & 0x0F
	}

	var states []VICII
	for _, raster := range []uint16{0, 1, badLineRasterStart - 1, badLineRasterStart,
		badLineRasterStart + 1, 100, 101, badLineRasterEnd, badLineRasterEnd + 1, 300} {
		for _, allow := range []bool{false, true} {
			for _, idle := range []bool{false, true} {
				for _, control1 := range []uint8{0x00, 0x03, 0x10, 0x14, 0x1b} {
					var v VICII
					v.Reset()
					v.rasterLine = raster
					v.allowBadLine = allow
					v.idle = idle
					v.control1 = control1
					v.VC = 0x14
					v.VCBase = 0x14
					v.VMLI = 3
					v.RC = 5
					v.syncLineVisibility()
					states = append(states, v)
				}
			}
		}
	}
	return states
}

func TestPhi0LowDisplayMatchesPhi0Low(t *testing.T) {
	for _, state := range displayRunStates(t) {
		for slot := uint16(displayFirstSlot); slot < displaySlotAfter; slot++ {
			general := state
			general.phi0low(slot)

			display := state
			display.phi0lowDisplay()

			if general != display {
				t.Fatalf("phi0lowDisplay differs from phi0low at slot %d, raster %d, control1 $%02x, allowBadLine %v, idle %v",
					slot, state.rasterLine, state.control1, state.allowBadLine, state.idle)
			}
		}
	}
}

func TestPhi0HighDisplayMatchesPhi0High(t *testing.T) {
	for _, state := range displayRunStates(t) {
		for _, badLine := range []bool{false, true} {
			for _, ba := range []bool{false, true} {
				base := state
				base.badLine = badLine
				base.BA = ba

				for slot := uint16(displayFirstSlot); slot < displaySlotAfter; slot++ {
					general := base
					// phi0high is called with slot+1: see its comment.
					general.phi0high(slot + 1)

					display := base
					display.phi0highDisplay()

					if general != display {
						t.Fatalf("phi0highDisplay differs from phi0high at slot %d, raster %d, badLine %v, BA %v",
							slot, state.rasterLine, badLine, ba)
					}
				}
			}
		}
	}
}

// The run's bounds are what make the tests above meaningful: they hold
// that the half-phases agree on the slots the run covers, not that the
// run covers the right slots.
func TestDisplayRunStaysWithinTheHoistedRanges(t *testing.T) {
	for slot := uint16(displayFirstSlot); slot < displaySlotAfter; slot++ {
		if slot == 52 || slot == 53 || slot == 47 || (slot >= 1 && slot <= 3) {
			t.Fatalf("slot %d does per-slot work in phi0low but is inside the display run", slot)
		}
		if !(slot >= 1 && slot <= 43) {
			t.Fatalf("slot %d is outside phi0low's BA range but inside the display run", slot)
		}
		if !(slot >= 5 && slot <= 44) {
			t.Fatalf("slot %d is outside phi0low's g-access range but inside the display run", slot)
		}
		// phi0high is called with slot+1.
		if next := slot + 1; !(next >= 5 && next <= 44) {
			t.Fatalf("slot %d is outside phi0high's c-access range but inside the display run", slot)
		}
		if !(slot >= reloadFirstSlot && slot < reloadSlotAfter) {
			t.Fatalf("slot %d is outside the reload window but inside the display run", slot)
		}
		if slot == borderSlotLeft || slot == borderSlotRight38 || slot == borderSlotRight40 {
			t.Fatalf("slot %d can match the border comparator but is inside the display run", slot)
		}
	}
	if displaySlotAfter >= CyclesPerLine {
		t.Fatalf("the display run reaches the end of a line, so it needs the wrap test")
	}
}

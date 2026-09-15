package tiny64

import "testing"

func TestSpriteAECWarning(t *testing.T) {
	for sprite := uint8(0); sprite < 8; sprite++ {
		t.Run(string(rune('0'+sprite)), func(t *testing.T) {
			newMachine(t)
			savedBus := bus
			t.Cleanup(func() { bus = savedBus })
			vic.rasterLine, vic.dot = 56, 43*DotsPerCycle
			vic.WriteRegister(0xD001+uint16(sprite)*2, 56)
			vic.WriteRegister(0xD015, 1<<sprite)
			vic.control1 = 0 // No badline.
			vic.syncLineVisibility()
			cpu.PC = 0x0200
			for i := 0; i < 64; i++ {
				ram[0x0200+i] = 0xEA
			}
			first := uint16(44) + 2*uint16(sprite)
			for slot := uint16(43); slot <= 63; slot++ {
				vic.StepCycle()
				wantBA := slot < first || slot > first+4
				wantAEC := slot < first+3 || slot > first+4
				if vic.BA != wantBA || vic.AEC != wantAEC {
					t.Fatalf("slot %d: BA/AEC=%v/%v, want %v/%v",
						slot, vic.BA, vic.AEC, wantBA, wantAEC)
				}
			}
		})
	}
}

func TestOverlappingSpriteBAKeepsWarningElapsed(t *testing.T) {
	newMachine(t)
	savedBus := bus
	t.Cleanup(func() { bus = savedBus })
	vic.rasterLine, vic.dot = 56, 44*DotsPerCycle
	vic.WriteRegister(0xD001, 56)
	vic.WriteRegister(0xD003, 56)
	vic.WriteRegister(0xD015, 3)
	vic.control1 = 0
	vic.syncLineVisibility()
	cpu.PC = 0x0200
	ram[0x0200] = 0xEA
	for slot := 44; slot <= 51; slot++ {
		vic.StepCycle()
		wantBA := slot == 51
		wantAEC := slot < 47 || slot == 51
		if vic.BA != wantBA || vic.AEC != wantAEC {
			t.Fatalf("slot %d: overlapping DMA BA/AEC=%v/%v, want %v/%v",
				slot, vic.BA, vic.AEC, wantBA, wantAEC)
		}
	}
}

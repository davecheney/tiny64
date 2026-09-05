package tiny64

import (
	"testing"

	"github.com/davecheney/tiny64/rom"
)

func TestDbgVectors(t *testing.T) {
	t.Logf("RESET vector: $%02X%02X", rom.Kernal[0xFFFD-0xE000], rom.Kernal[0xFFFC-0xE000])
	t.Logf("IRQ   vector: $%02X%02X", rom.Kernal[0xFFFF-0xE000], rom.Kernal[0xFFFE-0xE000])
	t.Logf("NMI   vector: $%02X%02X", rom.Kernal[0xFFFB-0xE000], rom.Kernal[0xFFFA-0xE000])
}

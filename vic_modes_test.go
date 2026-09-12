package tiny64

import "testing"

func TestVICGraphicsModeColors(t *testing.T) {
	v := &VICII{
		background0:        1,
		registers22To2E:    [0x0D]uint8{2, 3, 4},
		gdPending:          0x1B, // 00, 01, 10, 11
		videoBufferPending: 0x0D00 | 0xC1,
	}

	testMode := func(name string, control1, control2 uint8, want []byte) {
		t.Helper()
		v.control1, v.control2 = control1, control2
		v.loadGraphicsData()
		for i, color := range want {
			if got := v.nextGraphicsColor(); got != color {
				t.Errorf("%s pixel %d = %d, want %d", name, i, got, color)
			}
		}
	}

	// The high Color RAM bit selects multicolor text. Clear it to prove
	// MCM alone still uses standard text for that character.
	v.videoBufferPending = 0x0500 | 0xC1
	testMode("standard text in MCM", 0, 0x10, []byte{1, 1, 1, 5, 5, 1, 5, 5})

	v.videoBufferPending = 0x0D00 | 0xC1
	testMode("multicolor text", 0, 0x10, []byte{1, 1, 2, 2, 3, 3, 5, 5})

	v.videoBufferPending = 0x00D6
	testMode("standard bitmap", 0x20, 0, []byte{6, 6, 6, 13, 13, 6, 13, 13})

	v.videoBufferPending = 0x0D06
	testMode("multicolor bitmap", 0x20, 0x10, []byte{1, 1, 0, 0, 6, 6, 13, 13})

	v.videoBufferPending = 0x0D00 | 0xC1
	testMode("ECM text", 0x40, 0, []byte{4, 4, 4, 13, 13, 4, 13, 13})
}

func TestVICGraphicsModeAddresses(t *testing.T) {
	saveMachine(t)
	cia2.PRA, cia2.DDRA = 3, 3 // VIC bank 0

	v := &VICII{RC: 3, VC: 12, memPointers: 0x08}
	v.videoMatrixColor[0] = 0x00C1

	ram[0x2063] = 0xA1
	v.control1, v.control2 = 0x20, 0
	v.cycleGAccess()
	if got := v.gdPending; got != 0xA1 {
		t.Fatalf("standard bitmap g-access = %#02x, want %#02x", got, 0xA1)
	}

	ram[0x200B] = 0xB2
	v.VC = 0
	v.VMLI = 0
	v.control1, v.control2 = 0x40, 0
	v.cycleGAccess()
	if got := v.gdPending; got != 0xB2 {
		t.Fatalf("ECM text g-access = %#02x, want %#02x", got, 0xB2)
	}
}

func TestVICXScrollDelaysGraphicsReload(t *testing.T) {
	v := &VICII{control2: 3, gdPending: 0xFF, videoBufferPending: 0x0100}

	v.dot = 48 // start of slot 6, where unscrolled data would reload
	v.advanceGraphicsData()
	if v.gdSequencer != 0 {
		t.Fatalf("sequencer reloaded at dot %d with XSCROLL=3", v.dot)
	}

	v.dot = 51
	v.advanceGraphicsData()
	if v.gdSequencer != 0xFF || v.videoBuffer != 0x0100 {
		t.Fatalf("sequencer=%#02x buffer=%#04x at dot %d, want pending graphics data",
			v.gdSequencer, v.videoBuffer, v.dot)
	}
}

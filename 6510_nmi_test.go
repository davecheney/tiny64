package tiny64

import "testing"

// These drive the CPU directly, with no ROM and no KERNAL, to pin down the
// three properties that distinguish NMI from IRQ: it ignores the I flag,
// it wins when both are pending, and it is edge-triggered rather than
// level-triggered.
//
// Addresses used by the fixture:
//
//	$0010  count of NMIs taken
//	$0011  count of IRQs taken
//	$0012  value of $0011 as the NMI handler entered, so a test can tell
//	       which of the two ran first
//	$0200  the interrupted program: a one-instruction loop
//	$0300  NMI handler
//	$0400  IRQ handler
func newNMIFixture(t *testing.T) {
	saveMachine(t)

	ram = [65536]byte{}
	bus = Bus{}
	cpu = CPU{}
	cpu.PortDDR = 0xFF // LORAM/HIRAM/CHAREN driven low: plain RAM everywhere, so the vectors are writable
	cia1 = CIA{}
	cia2 = CIA{}
	keyboard = Keyboard{}
	vic = VICII{}
	vic.BA, vic.AEC = true, true

	copy(ram[0x0200:], []uint8{0x4C, 0x00, 0x02}) // JMP $0200
	copy(ram[0x0300:], []uint8{
		0xA5, 0x11, // LDA $11
		0x85, 0x12, // STA $12
		0xE6, 0x10, // INC $10
		0x40, // RTI
	})
	copy(ram[0x0400:], []uint8{
		0xE6, 0x11, // INC $11
		0x40, // RTI
	})
	ram[0xFFFA], ram[0xFFFB] = 0x00, 0x03
	ram[0xFFFE], ram[0xFFFF] = 0x00, 0x04

	cpu.PC = 0x0200
	cpu.SP = 0xFF
}

func runCycles(n int) {
	for range n {
		cpu.TickPhi2()
	}
}

// The I flag masks IRQ and nothing else, which is what "non-maskable"
// means. Both lines are asserted here with interrupts disabled: the NMI
// must still be taken, and the IRQ must not.
func TestNMIIgnoresInterruptDisable(t *testing.T) {
	newNMIFixture(t)
	cpu.regP = P_INTERRUPT

	cia1.setIRQ(true)
	cia2.setIRQ(true)
	runCycles(100)

	if ram[0x0010] != 1 {
		t.Errorf("NMIs taken with I set = %d, want 1", ram[0x0010])
	}
	if ram[0x0011] != 0 {
		t.Errorf("IRQs taken with I set = %d, want 0", ram[0x0011])
	}
}

// With both pending and interrupts enabled, both handlers eventually run,
// but NMI has to go first. The IRQ then follows once the NMI handler's
// RTI restores I=0, since CIA1 is still holding its line asserted.
func TestNMIHasPriorityOverIRQ(t *testing.T) {
	newNMIFixture(t)

	cia1.setIRQ(true)
	cia2.setIRQ(true)
	runCycles(200)

	if ram[0x0010] != 1 {
		t.Fatalf("NMIs taken = %d, want 1", ram[0x0010])
	}
	if ram[0x0011] == 0 {
		t.Fatal("IRQ was never taken at all, so this proves nothing about ordering")
	}
	if ram[0x0012] != 0 {
		t.Errorf("IRQ count on entry to the NMI handler = %d, want 0: the IRQ was serviced first", ram[0x0012])
	}
}

// NMI is edge-triggered. A source that holds the line asserted (CIA2 with
// an interrupt the handler never acknowledges, which is exactly what this
// fixture's handler does) must produce one NMI, not one per instruction.
// Dropping the line and raising it again is a new edge and must produce a
// second.
func TestNMIIsEdgeTriggered(t *testing.T) {
	newNMIFixture(t)

	cia2.setIRQ(true)
	runCycles(2000)
	if ram[0x0010] != 1 {
		t.Fatalf("NMIs taken while the line was held asserted = %d, want 1", ram[0x0010])
	}

	cia2.setIRQ(false)
	runCycles(20)
	cia2.setIRQ(true)
	runCycles(100)
	if ram[0x0010] != 2 {
		t.Errorf("NMIs taken after a second edge = %d, want 2", ram[0x0010])
	}
}

// RESTORE latches an NMI directly, so its press-edge event must deliver an
// NMI on its own, with CIA2 idle throughout.
func TestRestoreDeliversNMI(t *testing.T) {
	newNMIFixture(t)

	keyboard.Restore()
	runCycles(100)

	if ram[0x0010] != 1 {
		t.Errorf("NMIs taken after RESTORE = %d, want 1", ram[0x0010])
	}
	if cia2.IRQ {
		t.Error("CIA2 asserted its interrupt line, so this test did not exercise RESTORE in isolation")
	}
}

// Rebuilding the keyboard matrix after a RESTORE press must not affect the
// already-latched event: the NMI has no level to hold, so there is nothing
// for ReleaseAll to disturb.
func TestRestoreSurvivesKeyboardRelease(t *testing.T) {
	newNMIFixture(t)

	keyboard.Restore()
	keyboard.ReleaseAll()
	runCycles(100)

	if ram[0x0010] != 1 {
		t.Errorf("NMIs taken after RESTORE = %d, want 1", ram[0x0010])
	}
}

package tiny64

import "testing"

// autostartTestPRG is 10 PRINT"ZZTOP", a BASIC program that loads at
// $0801 and leaves a mark on the screen. "it loaded" and "it ran" are
// different claims, and only a program that prints something can tell
// them apart.
var autostartTestPRG = []byte{
	0x01, 0x08, // load address $0801
	0x0E, 0x08, // link to the end-of-program marker at $080E
	0x0A, 0x00, // line 10
	0x99,                              // PRINT
	'"', 'Z', 'Z', 'T', 'O', 'P', '"', // "ZZTOP"
	0x00,       // end of line
	0x00, 0x00, // end of program
}

// TestAutostartLoadsAndRunsTheFirstFileOnTheDisk is the end-to-end case:
// a real KERNAL boot, a real BASIC LOAD over the serial bus, and a RUN
// that was typed before the load had even started. It runs against
// whichever drive is compiled in, so -tags drive1541 exercises the same
// sequence against a drive that answers every handshake on the wire.
func TestAutostartLoadsAndRunsTheFirstFileOnTheDisk(t *testing.T) {
	skipShort(t)

	m := newMachine(t)
	InsertDisk(virtualDriveDisk(t, "RUNME", autostartTestPRG))
	t.Cleanup(func() { InsertDisk(nil) })

	Autostart()

	// The editor echoed what the host put in its buffer.
	m.waitForScreen(`LOAD"*",8,1`)

	// The KERNAL and the drive did the load between them.
	m.waitForScreen("SEARCHING FOR *")
	m.waitForScreen("LOADING")

	// And the queued RUN started the program afterwards.
	m.waitForScreen("ZZTOP")
}

// TestAutostartQueuesRunBehindTheLoad pins the idea the whole design
// rests on: both commands go into the KERNAL's ten-character buffer up
// front, and the RUN survives there for the duration of the load. By the
// time Autostart returns the host has no work left to do - BASIC has the
// LOAD and the editor is still holding the RUN.
func TestAutostartQueuesRunBehindTheLoad(t *testing.T) {
	skipShort(t)

	m := newMachine(t)
	InsertDisk(virtualDriveDisk(t, "RUNME", autostartTestPRG))
	t.Cleanup(func() { InsertDisk(nil) })

	Autostart()

	if got, want := string(ram[kernalKeyBuffer:kernalKeyBuffer+4]), "RUN\r"; got != want {
		t.Fatalf("keyboard buffer = %q, want %q", got, want)
	}
	if got := ram[kernalKeyCount]; got != 4 {
		t.Fatalf("NDX = %d, want 4 characters still waiting", got)
	}

	// Nothing has loaded yet, so the RUN really is queued ahead of it.
	if screenHas("LOADING") {
		t.Fatal("the load had already finished when Autostart returned")
	}

	m.waitForScreen("ZZTOP")
}

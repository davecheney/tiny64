package tiny64

import (
	"bytes"
	"testing"
)

// TestDriveBootsToIdleLoop checks the 1541's own ROM comes up on its own
// CPU: a real drive runs its power-on self test, prints "73,CBM DOS V2.6
// 1541" into its error buffer, and settles into the DOS idle loop waiting
// for something on the bus to talk to it.
func TestDriveBootsToIdleLoop(t *testing.T) {
	m := newMachine(t)
	AttachDrive(true)
	m.run(2_000_000)

	if got, want := string(driveRAM[0x02D5:0x02EF]), "73,CBM DOS V2.6 1541,00,00"; got != want {
		t.Errorf("drive error buffer = %q, want %q", got, want)
	}
	if driveCPU.PC < 0xEC00 || driveCPU.PC > 0xEC80 {
		t.Errorf("drive PC = $%04X, want it idling in the DOS main loop around $EC33", driveCPU.PC)
	}
}

// TestDriveATNInterrupt checks the wiring that lets the C64 get the
// drive's attention at all. ATN reaches VIA1's CA1 pin through an
// inverter, and the DOS enables a CA1 interrupt for exactly this reason,
// so pulling ATN low has to drag the drive out of its idle loop and into
// the ATN command handler at $E85B.
func TestDriveATNInterrupt(t *testing.T) {
	m := newMachine(t)
	AttachDrive(true)
	m.run(2_000_000)

	SetCIA2ATN(true)
	for range 2000 {
		driveTickPhi2()
		if driveCPU.PC >= 0xE85B && driveCPU.PC < 0xE8D0 {
			return
		}
	}
	t.Fatalf("after asserting ATN the drive is at $%04X, want it in the ATN handler at $E85B", driveCPU.PC)
}

// TestDriveWritesTrackBackToDisk checks the write half of the head model.
// Nothing in a D64 records GCR, so a sector the drive writes only survives
// if the track image under the head is decoded back into sectors, which is
// what happens when the head steps away or the motor stops.
func TestDriveWritesTrackBackToDisk(t *testing.T) {
	m := newMachine(t)
	m.run(10_000)

	InsertDisk(testDisk("TEST DISK", "42"))
	t.Cleanup(func() { InsertDisk(nil) })

	// Put the head over track 18 and lay down a sector by hand, exactly
	// as the drive's write logic would have left it.
	driveHalfTrack = 18 * 2
	ensureTrackData()

	var block [256]byte
	for i := range block {
		block[i] = byte(i)
	}
	id1, id2 := diskID()
	sector := gcrEncodeSector(nil, 18, 5, id1, id2, &block, 8)
	for i, b := range sector {
		driveTrackData[i%len(driveTrackData)] = b
	}
	driveTrackDirty = true

	driveFlushTrack()

	got := diskReadSector(18, 5)
	if got == nil {
		t.Fatal("track 18 sector 5 is missing from the image")
	}
	if !bytes.Equal(got[:], block[:]) {
		t.Errorf("track 18 sector 5 = %x..., want %x...", got[:16], block[:16])
	}
}

// TestLoadDirectory is the whole disk system end to end: someone types
// LOAD"$",8 at the BASIC prompt, the KERNAL talks to the drive over the
// serial bus, the drive's DOS reads the directory off the GCR image of
// track 18, sends it back a byte at a time, and LIST shows it.
func TestLoadDirectory(t *testing.T) {
	m := newMachine(t)
	InsertDisk(testDisk("TEST DISK", "42", "HELLO"))
	t.Cleanup(func() { InsertDisk(nil) })

	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"$",8`)
	m.run(3_000_000)
	m.typeLine("LIST")
	m.run(400_000)

	for _, want := range []struct {
		row  int
		text string
	}{
		{8, "SEARCHING FOR $"},
		{9, "LOADING"},
		{13, `0 "TEST DISK       " 42 2A`},
		{14, `1    "HELLO"            PRG`},
		{15, "664 BLOCKS FREE."},
	} {
		if got := screenLine(want.row); got != want.text {
			t.Errorf("screen row %d = %q, want %q", want.row, got, want.text)
		}
	}
}

// TestFormatDisk lets the drive's own DOS format a blank disk, which is
// the only way to be sure the write path really works: the DOS lays down
// every sector header and data block itself, reads them back to verify,
// and refuses to finish if anything it wrote does not decode. Formatting
// 35 tracks takes about ninety seconds of drive time, so this is not a
// test to run on every save.
func TestFormatDisk(t *testing.T) {
	if testing.Short() {
		t.Skip("formatting a disk takes ninety seconds of emulated drive time")
	}

	m := newMachine(t)
	InsertDisk(NewDisk())
	t.Cleanup(func() { InsertDisk(nil) })

	m.waitForLine(5, "READY.")
	m.typeLine(`OPEN15,8,15,"N0:TINY64,64"`)
	m.run(95_000_000)
	m.typeLine("CLOSE15")
	m.run(500_000)

	DiskImage() // flush whatever the drive left under its head
	bam := diskReadSector(18, 0)
	if bam == nil {
		t.Fatal("no BAM on track 18 sector 0 after formatting")
	}
	if bam[0] != 18 || bam[1] != 1 {
		t.Errorf("BAM directory link = %d/%d, want 18/1", bam[0], bam[1])
	}
	if got, want := string(bam[0x90:0x96]), "TINY64"; got != want {
		t.Errorf("disk name = %q, want %q", got, want)
	}
	if got, want := string(bam[0xA2:0xA4]), "64"; got != want {
		t.Errorf("disk ID = %q, want %q", got, want)
	}
	if got, want := string(bam[0xA5:0xA7]), "2A"; got != want {
		t.Errorf("DOS type = %q, want %q", got, want)
	}
}

// testDisk builds a formatted 35-track D64 with the given name and ID and
// a closed PRG entry for each file, so tests that only want to read a
// directory do not have to spend ninety seconds of drive time formatting
// one first. TestFormatDisk covers the drive doing it for real.
func testDisk(name, id string, files ...string) []byte {
	img := NewDisk()

	bam := make([]byte, 256)
	bam[0], bam[1] = 18, 1 // link to the first directory sector
	bam[2] = 0x41          // DOS version 'A'
	for track := 1; track <= 35; track++ {
		n := sectorsPerTrack(uint8(track))
		free, bits := n, (1<<uint(n))-1
		if track == 18 {
			free, bits = n-2, bits&^0x03 // the BAM and one directory sector
		}
		e := 4 + (track-1)*4
		bam[e] = byte(free)
		bam[e+1], bam[e+2], bam[e+3] = byte(bits), byte(bits>>8), byte(bits>>16)
	}
	for i := 0x90; i <= 0xAA; i++ {
		bam[i] = 0xA0 // shifted spaces, the way the DOS pads its fields
	}
	copy(bam[0x90:0xA0], name)
	copy(bam[0xA2:0xA4], id)
	bam[0xA5], bam[0xA6] = '2', 'A'
	copy(img[trackOffset(18):], bam)

	dir := make([]byte, 256)
	dir[0], dir[1] = 0, 0xFF // no further directory sectors
	for i, f := range files {
		e := i * 32
		dir[e+2] = 0x82            // a closed PRG
		dir[e+3], dir[e+4] = 17, 0 // first data block
		for j := range 16 {
			dir[e+5+j] = 0xA0
		}
		copy(dir[e+5:e+21], f)
		dir[e+30], dir[e+31] = 1, 0 // one block long
	}
	copy(img[trackOffset(18)+256:], dir)

	return img
}

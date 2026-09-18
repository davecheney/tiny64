//go:build drive1541

package tiny64

import (
	"bytes"
	"fmt"
	"testing"
)

// TestDriveBootsToIdleLoop checks the 1541's own ROM comes up on its own
// CPU: a real drive runs its power-on self test, prints "73,CBM DOS V2.6
// 1541" into its error buffer, and settles into the DOS idle loop waiting
// for something on the bus to talk to it.
func TestDriveBootsToIdleLoop(t *testing.T) {
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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
	skipShort(t)

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

// TestLoadFile loads an actual program file rather than the directory,
// and checks every byte of it arrives. A bit-banged transfer that drops a
// bit still looks like it worked - the KERNAL prints LOADING either way -
// so the only real test is comparing what landed in RAM against what is
// on the disk.
//
// The load is repeated at a range of raster phases because the C64's
// receive loop is level-triggered and a Bad Line can stall it for the
// better part of a bit time; a transfer that only works at one phase
// works by luck.
func TestLoadFile(t *testing.T) {
	skipShort(t)

	for _, skew := range []int{0, 13, 34, 55, 89, 144} {
		t.Run(fmt.Sprintf("skew%d", skew), func(t *testing.T) {
			m := newMachine(t)
			InsertDisk(testDisk("TEST DISK", "42", "HELLO"))
			t.Cleanup(func() { InsertDisk(nil) })

			m.waitForLine(5, "READY.")
			m.run(skew)
			m.typeLine(`LOAD"HELLO",8,1`)
			m.run(4_000_000)

			if got := screenLine(9); got != "LOADING" {
				t.Fatalf("screen row 9 = %q, want %q", got, "LOADING")
			}

			// A ,8,1 load puts the file at the address in its first two
			// bytes rather than at the start of BASIC.
			want := testFileContents("HELLO")
			addr := int(want[0]) | int(want[1])<<8
			body := want[2:]
			if got := ram[addr : addr+len(body)]; !bytes.Equal(got, body) {
				for i := range body {
					if got[i] != body[i] {
						t.Fatalf("byte %d of %d differs: RAM $%04X = %02X, disk = %02X", i, len(body), addr+i, got[i], body[i])
					}
				}
			}
		})
	}
}

// TestSaveAndReload writes a program to disk from BASIC and reads it back.
// SAVE exercises a different path from either of the other write tests:
// the KERNAL sends the file over the bus, the DOS finds free blocks in the
// BAM, writes the data and directory sectors itself, and the track only
// becomes part of the D64 again when it is decoded back out of GCR. The
// KERNAL prints SAVING whatever happens, so the test is the reload.
func TestSaveAndReload(t *testing.T) {
	skipShort(t)

	m := newMachine(t)
	InsertDisk(testDisk("TEST DISK", "42"))
	t.Cleanup(func() { InsertDisk(nil) })

	m.waitForLine(5, "READY.")
	m.typeLine("10 PRINT 1")
	m.typeLine(`SAVE"NEW",8`)
	m.run(20_000_000)

	// The directory entry has to be a closed PRG, or the DOS wrote
	// something it will refuse to read back.
	DiskImage() // flush the track still under the head
	dir := diskReadSector(18, 1)
	if dir == nil {
		t.Fatal("no directory sector on track 18")
	}
	if got, want := dir[2], uint8(0x82); got != want {
		t.Errorf("directory entry type = $%02X, want $%02X (closed PRG)", got, want)
	}
	if got, want := string(dir[5:8]), "NEW"; got != want {
		t.Errorf("directory entry name = %q, want %q", got, want)
	}

	// Wipe the program out of memory, so a successful LIST can only have
	// come off the disk.
	m.typeLine("NEW")
	m.typeLine(`LOAD"NEW",8`)
	m.run(3_000_000)
	m.typeLine("LIST")
	m.run(400_000)

	if got, want := screenLine(21), "10 PRINT 1"; got != want {
		t.Errorf("reloaded program listed as %q, want %q", got, want)
	}
}

// TestDiskInsertedBeforeReset covers the order the front ends use: both
// cmd/c64 and cmd/c64cli read the image named by -disk and insert it
// before resetting the machine, where every other test here resets first
// and inserts afterwards. Inserting is what plugs the drive in, so the
// two orders put the reset and the attach the other way round.
func TestDiskInsertedBeforeReset(t *testing.T) {
	skipShort(t)

	saveMachine(t)
	for i := range ram {
		ram[i] = 0xAA // stand-in for the power-on noise the front ends write
	}

	InsertDisk(testDisk("TEST DISK", "42", "HELLO"))
	t.Cleanup(func() { InsertDisk(nil) })
	Reset()

	m := &machine{t: t}
	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"$",8`)
	m.run(3_000_000)
	m.typeLine("LIST")
	m.run(400_000)

	if got, want := screenLine(13), `0 "TEST DISK       " 42 2A`; got != want {
		t.Errorf("screen row 13 = %q, want %q", got, want)
	}
}

// TestFormatDisk lets the drive's own DOS format a blank disk, which is
// the only way to be sure the write path really works: the DOS lays down
// every sector header and data block itself, reads them back to verify,
// and refuses to finish if anything it wrote does not decode. Formatting
// 35 tracks takes about ninety seconds of drive time, so this is not a
// test to run on every save.
func TestFormatDisk(t *testing.T) {
	skipShort(t)

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
//
// Each file gets one data block on track 17, holding testFileContents.
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
		dir[e+3], dir[e+4] = 17, uint8(i) // its one data block
		dir[e+30], dir[e+31] = 1, 0       // one block long

		block := make([]byte, 256)
		body := testFileContents(f)
		block[0], block[1] = 0, uint8(len(body)+1) // last block, and how much of it is used
		copy(block[2:], body)
		copy(img[trackOffset(17)+i*256:], block)
	}
	copy(img[trackOffset(18)+256:], dir)

	return img
}

// testFileContents returns the bytes testDisk stores for a file: a PRG
// load address of $C000 followed by a run of values derived from the name,
// so a test can tell one file's contents from another's.
func testFileContents(name string) []byte {
	body := []byte{0x00, 0xC0} // load address $C000, little-endian
	for i := range 64 {
		body = append(body, byte(i)^name[i%len(name)])
	}
	return body
}

// TestLoadFileWithSyncLikeData loads a file whose GCR image contains a
// byte of $FF that is not a SYNC mark.
//
// GCR's longest run of one bits is eight - $5 encodes to 01111 and $E to
// 11110 - so data can produce a whole $FF byte whenever such a run lands
// on a byte boundary. A SYNC is ten or more ones, which data cannot make.
// A drive that tests for $FF a byte at a time rather than counting bits
// mistakes those bytes for SYNC and withholds them from the DOS, which
// loses one byte out of the block and fails its checksum. The symptom is
// a file that stops part way through and a LOAD that then hangs forever,
// because the drive gives up while the C64 is still waiting for bytes.
//
// The contents are chosen to put $5 and $E next to each other over and
// over: an alternating 05 E0 puts the low nibble of an $05 immediately
// before the high nibble of an $E0, which is 01111 followed by 11110.
func TestLoadFileWithSyncLikeData(t *testing.T) {
	skipShort(t)

	body := []byte{0x00, 0xC0} // load address $C000
	for i := len(body); i < 254; i++ {
		if i%2 == 0 {
			body = append(body, 0x05)
		} else {
			body = append(body, 0xE0)
		}
	}

	img := testDisk("TEST DISK", "42", "SYNCBUG")
	block := make([]byte, 256)
	block[0], block[1] = 0, uint8(len(body)+1)
	copy(block[2:], body)
	copy(img[trackOffset(17):], block)

	m := newMachine(t)
	InsertDisk(img)
	t.Cleanup(func() { InsertDisk(nil) })

	// Without a lone $FF in the track there is nothing here to get wrong,
	// so check the contents really do provoke it before trusting the load.
	track := diskEncodeTrack(17)
	lone := 0
	for i, b := range track {
		if b != 0xFF {
			continue
		}
		if track[(i+len(track)-1)%len(track)] != 0xFF && track[(i+1)%len(track)] != 0xFF {
			lone++
		}
	}
	if lone == 0 {
		t.Fatal("no isolated $FF in the encoded track, so this test cannot fail")
	}

	m.waitForLine(5, "READY.")
	m.typeLine(`LOAD"SYNCBUG",8,1`)
	m.run(8_000_000)

	if got := screenLine(10); got != "READY." {
		t.Fatalf("screen row 10 = %q, want %q - the load did not finish", got, "READY.")
	}
	addr, want := 0xC000, body[2:]
	if got := ram[addr : addr+len(want)]; !bytes.Equal(got, want) {
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("byte %d of %d differs: RAM $%04X = %02X, disk = %02X", i, len(want), addr+i, got[i], want[i])
			}
		}
	}
}

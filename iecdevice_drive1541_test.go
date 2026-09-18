//go:build drive1541

package tiny64

// The two drives only have something to agree about in a build that has
// both of them. These compare the generic drive against the 1541 running
// its real DOS ROM, which is the only oracle that says the generic one is
// right rather than merely self-consistent.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDrivesAgreeOnDirectoryText compares the directory the generic drive
// synthesises against the one the 1541 produces, on the same image, in
// the same machine, byte for byte on the screen.
//
// The 1541 is the oracle here, and not merely because it came first: the
// text it puts on the screen is formatted by the DOS ROM itself, so it is
// the real drive's output by construction. The generic drive
// reimplements that formatting in Go, so it is the side that can drift.
// This test is the cheapest possible check on the whole DOS layer,
// because anything the ROM does that the Go does not is a screen diff.
//
// The names are deliberately sixteen characters, the width of the name
// field, because that is the length that pins the column after it: the
// padding loop runs zero times and only the closed-file flag is left, so
// an off-by-one in the padding cannot hide behind a short name.
func TestDrivesAgreeOnDirectoryText(t *testing.T) {
	skipShort(t)

	saveMachine(t)
	disk := FormatDisk("COMPARE DISK", "01")
	diskImage = disk
	for _, name := range []string{
		"04.LAST NIGHT 30", // exactly sixteen, the pinning case
		"A",                // one, the longest padding run
		"MIDDLING NAME",
	} {
		if code := diskWriteFile(name, ftypePRG, helloPRG); code != 0 {
			t.Fatalf("writing %q: DOS error %d", name, code)
		}
	}

	// A $A0 is written into the header field below, because that is what
	// CBM DOS pads it with and it is the case the real images here do not
	// isolate: it must reach the screen as a space, since LIST renders
	// $A0 in a BASIC line as the token CLOSE.
	if bam := diskReadSector(dirTrack, 0); bam != nil {
		bam[0xA4] = 0xA0
	}

	real, virtual := listDirectoryOnBothDrives(t, disk)

	// Two blank screens compare equal, so the oracle has to be shown to
	// have said something before its agreement means anything.
	if !containsAny(real, "COMPARE DISK") {
		t.Fatalf("the 1541 listed no directory header, so the comparison proved nothing: %q", real)
	}
	for i := range real {
		if real[i] != virtual[i] {
			t.Errorf("screen row %d differs:\n1541    %q\nvirtual %q", i, real[i], virtual[i])
		}
	}
}

// TestDrivesAgreeOnRealDisks runs the same comparison as
// TestDrivesAgreeOnDirectoryText against images this repository did not
// produce.
//
// This is the stronger form of that test and it is worth having both. A
// synthesised disk is written by the same code that reads it, so the two
// can share an assumption and agree while both being wrong; more
// importantly a generator only ever emits the shapes it knows how to
// emit. Every difference these images exposed was of that kind: a header
// whose five bytes are one string rather than an ID and a DOS version,
// DEL entries occupying live slots, and locked files. None of those can
// arise from FormatDisk and diskWriteFile however many files are written.
//
// The synthesised test is not redundant, because it covers the case real
// disks here do not reach as cheaply: a $A0 in the header, which must
// become a space before LIST renders it as the token CLOSE.
func TestDrivesAgreeOnRealDisks(t *testing.T) {
	skipShort(t)

	for _, name := range []string{"enforcer", "lastnight", "validated"} {
		t.Run(name, func(t *testing.T) {
			disk, err := os.ReadFile(filepath.Join("d64", name+".d64"))
			if err != nil {
				t.Skipf("no %s.d64 to compare against: %v", name, err)
			}
			saveMachine(t)
			real, virtual := listDirectoryOnBothDrives(t, disk)

			// Two blank screens compare equal, so the oracle has to be
			// shown to have said something before agreement means
			// anything.
			if !containsAny(real, "BLOCKS FREE.") {
				t.Fatalf("the 1541 listed no directory, so the comparison proved nothing: %q", real)
			}
			for i := range real {
				if real[i] != virtual[i] {
					t.Errorf("screen row %d differs:\n1541    %q\nvirtual %q", i, real[i], virtual[i])
				}
			}
		})
	}
}

// listDirectoryOnBothDrives returns the whole screen after listing disk's
// directory, once through the 1541 and once through the generic drive.
func listDirectoryOnBothDrives(t *testing.T, disk []byte) (real, virtual []string) {
	t.Helper()
	read := func(useVirtual bool) []string {
		m := newMachine(t)
		// Say which drive answers for device 8 outright rather than
		// leaning on InsertDisk's default: the whole point here is to run
		// the same disk past both implementations, so neither arm should
		// depend on which one this build would have picked.
		DetachVirtualDrive()
		if useVirtual {
			AttachDrive(false)
			AttachVirtualDrive(8)
		} else {
			AttachDrive(true)
		}
		InsertDisk(disk)
		m.waitForLine(5, "READY.")
		m.typeLine(`LOAD"$",8`)
		waitForLoad(m)
		m.typeLine("LIST")
		waitForListing(m)

		var out []string
		for row := range 25 {
			out = append(out, screenLine(row))
		}
		return out
	}
	return read(false), read(true)
}

func containsAny(rows []string, want string) bool {
	for _, r := range rows {
		if strings.Contains(r, want) {
			return true
		}
	}
	return false
}

package tiny64

import (
	"bytes"
	"testing"
)

func TestD64DirectoryAndReadFile(t *testing.T) {
	program := []byte{0x01, 0x08, 0x0B, 0x08, 0x0A, 0x00, 0x9E, '2', '0', '6', '4', 0x00, 0x00, 0x00}
	image, err := MakeD64FromPRG("hello.prg", program)
	if err != nil {
		t.Fatalf("MakeD64FromPRG: %v", err)
	}

	name, files, err := D64Directory(image)
	if err != nil {
		t.Fatalf("D64Directory: %v", err)
	}
	if name != "TINY64" {
		t.Errorf("disk name = %q, want %q", name, "TINY64")
	}
	if len(files) != 1 {
		t.Fatalf("got %d directory entries, want 1: %v", len(files), files)
	}
	got := files[0]
	if got.Name != "HELLO" || got.Type != "PRG" || !got.Closed || got.Locked {
		t.Errorf("entry = %+v, want HELLO PRG closed and unlocked", got)
	}
	if got.Blocks != 1 {
		t.Errorf("blocks = %d, want 1", got.Blocks)
	}

	for _, pattern := range []string{"HELLO", "HELL?", "HE*", "*"} {
		data, err := D64ReadFile(image, pattern)
		if err != nil {
			t.Fatalf("D64ReadFile(%q): %v", pattern, err)
		}
		if !bytes.Equal(data, program) {
			t.Errorf("D64ReadFile(%q) = % X, want % X", pattern, data, program)
		}
	}

	if _, err := D64ReadFile(image, "NOPE"); err == nil {
		t.Error("D64ReadFile(\"NOPE\") succeeded, want an error")
	}
}

// TestD64ReadDoesNotDisturbTheDrive checks that reading an image leaves the
// disk in the drive, and the drive's state, alone.
func TestD64ReadDoesNotDisturbTheDrive(t *testing.T) {
	saveMachine(t)
	InsertDisk(FormatDisk("INSERTED", "2A"))

	other, err := MakeD64FromPRG("other.prg", []byte{0x01, 0x08, 0x00, 0x00})
	if err != nil {
		t.Fatalf("MakeD64FromPRG: %v", err)
	}
	if _, _, err := D64Directory(other); err != nil {
		t.Fatalf("D64Directory: %v", err)
	}
	if name, _, err := D64Directory(DiskImage()); err != nil || name != "INSERTED" {
		t.Errorf("disk in the drive = %q, %v; want INSERTED", name, err)
	}
}

func TestD64ShortImage(t *testing.T) {
	short := make([]byte, 100)
	if _, _, err := D64Directory(short); err == nil {
		t.Error("D64Directory(short) succeeded, want an error")
	}
	if _, err := D64ReadFile(short, "*"); err == nil {
		t.Error("D64ReadFile(short) succeeded, want an error")
	}
}

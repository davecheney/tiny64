package tiny64

import "fmt"

// Read-only access to the CBM DOS filesystem inside a D64 image, for tools
// that want to look at a disk without running a drive. It reuses the same
// directory and sector-chain walkers the emulated drive uses, so what it
// reports is what the drive would read.

// D64File describes one directory entry of a D64 image.
type D64File struct {
	Name   string // CBM filename, $A0 padding removed
	Type   string // PRG, SEQ, USR, REL or DEL
	Blocks int    // size in 254 byte blocks, as the directory records it
	Closed bool   // false for a splat file, shown as *PRG by the DOS
	Locked bool
}

// withDisk runs fn with image installed as the disk the filesystem helpers
// operate on, restoring the previous image afterwards. The drive's cached
// track is left alone: nothing here writes.
func withDisk(image []byte, fn func()) {
	saved := diskImage
	diskImage = image
	defer func() { diskImage = saved }()
	fn()
}

// fileTypeName returns the CBM DOS name of a directory entry's type.
func fileTypeName(typ uint8) string {
	switch typ & ftypeMask {
	case ftypePRG:
		return "PRG"
	case ftypeSEQ:
		return "SEQ"
	case ftypeUSR:
		return "USR"
	case ftypeREL:
		return "REL"
	default:
		return "DEL"
	}
}

// D64Directory returns the disk name and directory listing of a D64 image.
func D64Directory(image []byte) (diskname string, files []D64File, err error) {
	if len(image) < D64Size {
		return "", nil, fmt.Errorf("tiny64: not a D64 image: %d bytes, want at least %d", len(image), D64Size)
	}
	withDisk(image, func() {
		diskname = diskName()
		for _, e := range diskDirectory() {
			files = append(files, D64File{
				Name:   e.nameString(),
				Type:   fileTypeName(e.typ),
				Blocks: int(e.blocks),
				Closed: e.closed(),
				Locked: e.typ&ftypeLocked != 0,
			})
		}
	})
	return diskname, files, nil
}

// D64ReadFile returns the contents of the first file in a D64 image whose
// name matches pattern, using CBM DOS wildcards: '?' matches any single
// character and '*' matches the rest of the name.
func D64ReadFile(image []byte, pattern string) ([]byte, error) {
	if len(image) < D64Size {
		return nil, fmt.Errorf("tiny64: not a D64 image: %d bytes, want at least %d", len(image), D64Size)
	}
	var (
		data  []byte
		found bool
	)
	withDisk(image, func() {
		var e dirEntry
		if e, found = diskFind(pattern, ftypeDEL); found {
			data = diskReadFile(e)
		}
	})
	if !found {
		return nil, fmt.Errorf("tiny64: %q: file not found on disk", pattern)
	}
	return data, nil
}

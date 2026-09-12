package tiny64

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The CBM DOS filesystem, as it sits inside a D64 image. None of this is
// how a 1541 does it - a 1541 works in GCR bitstreams and its DOS chases
// track/sector links through a 2K RAM buffer - but the on-disk structures
// are the same ones, so a disk written here is a disk a real 1541 reads.
//
// The layout: track 18 sector 0 is the BAM (block availability map) and
// carries the disk name and ID; track 18 sector 1 starts the directory, a
// chain of sectors each holding eight 32-byte slots; every file is itself
// a chain of sectors, each of which spends its first two bytes on the link
// to the next.

const (
	dirTrack     = 18
	dirFirstSec  = 1
	dirEntrySize = 32
	dirPerSector = 8

	// A file sector spends 2 bytes on the link to the next, leaving 254
	// for payload.
	sectorPayload = 254
)

// CBM file types, as stored in a directory entry's type byte. Bit 7 is
// the "closed properly" flag; a file without it shows up as a splat file.
const (
	ftypeDEL = 0
	ftypeSEQ = 1
	ftypePRG = 2
	ftypeUSR = 3
	ftypeREL = 4

	ftypeClosed = 0x80
	ftypeLocked = 0x40
	ftypeMask   = 0x07
)

// dirEntry is one directory slot, plus where it lives so it can be
// written back.
type dirEntry struct {
	typ    uint8
	track  uint8 // first sector of the file
	sector uint8
	name   [16]byte // padded with $A0
	blocks uint16

	// Where this slot lives on disk.
	slotTrack  uint8
	slotSector uint8
	slotIndex  int
}

// closed reports whether the file was closed properly (not a splat file).
func (e *dirEntry) closed() bool { return e.typ&ftypeClosed != 0 }

// fileType returns the entry's type with the flag bits stripped.
func (e *dirEntry) fileType() uint8 { return e.typ & ftypeMask }

// nameString returns the entry's name with the $A0 padding removed.
func (e *dirEntry) nameString() string {
	n := 0
	for n < len(e.name) && e.name[n] != 0xA0 && e.name[n] != 0x00 {
		n++
	}
	return string(e.name[:n])
}

// diskDirectory walks the directory chain and returns every non-empty
// slot. A slot whose type byte is zero is free, but the chain continues
// past it, so scratched files leave holes rather than truncating the
// listing.
func diskDirectory() []dirEntry {
	var entries []dirEntry
	track, sector := uint8(dirTrack), uint8(dirFirstSec)
	for visited := 0; track != 0 && visited < 1000; visited++ {
		block := diskReadSector(track, sector)
		if block == nil {
			break
		}
		for i := 0; i < dirPerSector; i++ {
			off := i * dirEntrySize
			if block[off+2] == 0 {
				continue
			}
			e := dirEntry{
				typ:        block[off+2],
				track:      block[off+3],
				sector:     block[off+4],
				blocks:     uint16(block[off+30]) | uint16(block[off+31])<<8,
				slotTrack:  track,
				slotSector: sector,
				slotIndex:  i,
			}
			copy(e.name[:], block[off+5:off+21])
			entries = append(entries, e)
		}
		track, sector = block[0], block[1]
	}
	return entries
}

// diskFind returns the first directory entry whose name matches pattern
// (CBM wildcards: * matches the rest of the name, ? matches any single
// character) and whose type matches typ. A typ of ftypeDEL matches any
// type, which is what the DOS does when the caller didn't say.
func diskFind(pattern string, typ uint8) (dirEntry, bool) {
	for _, e := range diskDirectory() {
		if e.fileType() == ftypeDEL {
			continue
		}
		if typ != ftypeDEL && e.fileType() != typ {
			continue
		}
		if cbmMatch(pattern, e.nameString()) {
			return e, true
		}
	}
	return dirEntry{}, false
}

// cbmMatch implements CBM DOS pattern matching: '?' matches exactly one
// character, '*' matches everything from there on, and a pattern that
// runs out before the name does does *not* match (unlike the name running
// out before the pattern, which also doesn't).
func cbmMatch(pattern, name string) bool {
	for i := 0; i < len(pattern); i++ {
		switch {
		case pattern[i] == '*':
			return true
		case i >= len(name):
			return false
		case pattern[i] == '?':
		case pattern[i] != name[i]:
			return false
		}
	}
	return len(pattern) == len(name)
}

// diskReadFile follows a file's sector chain and returns its contents. In
// the last sector of the chain the link's track byte is zero and its
// sector byte is the offset of the last byte used, so the tail is not
// padded out to a full block.
func diskReadFile(e dirEntry) []byte {
	var out []byte
	track, sector := e.track, e.sector
	for visited := 0; track != 0 && visited < 1000; visited++ {
		block := diskReadSector(track, sector)
		if block == nil {
			break
		}
		next, used := block[0], block[1]
		if next == 0 {
			if used < 2 {
				break
			}
			out = append(out, block[2:int(used)+1]...)
			break
		}
		out = append(out, block[2:]...)
		track, sector = next, used
	}
	return out
}

// bamOffset returns the offset of track's four BAM bytes within the BAM
// sector: a free-sector count followed by a 24-bit "is this sector free"
// bitmap, least significant bit first.
func bamOffset(track uint8) int { return 4 * int(track) }

func bamIsFree(bam *[256]byte, track, sector uint8) bool {
	off := bamOffset(track)
	return bam[off+1+int(sector)/8]&(1<<(sector%8)) != 0
}

func bamAllocate(bam *[256]byte, track, sector uint8) {
	off := bamOffset(track)
	mask := uint8(1) << (sector % 8)
	if bam[off+1+int(sector)/8]&mask != 0 {
		bam[off+1+int(sector)/8] &^= mask
		if bam[off] > 0 {
			bam[off]--
		}
	}
}

func bamRelease(bam *[256]byte, track, sector uint8) {
	off := bamOffset(track)
	mask := uint8(1) << (sector % 8)
	if bam[off+1+int(sector)/8]&mask == 0 {
		bam[off+1+int(sector)/8] |= mask
		bam[off]++
	}
}

// bamBlocksFree counts the free blocks across the whole disk. Track 18 is
// excluded: the BAM tracks its free sectors like any other, but the DOS
// reserves them for the directory and never reports them.
func bamBlocksFree(bam *[256]byte) int {
	free := 0
	for t := uint8(1); t <= 35; t++ {
		if t == dirTrack {
			continue
		}
		free += int(bam[bamOffset(t)])
	}
	return free
}

// bamFindFree picks the next free sector to allocate, working outwards
// from the directory track the way the real DOS does - files land next to
// the directory so the head has less distance to travel - and skipping
// track 18 itself.
func bamFindFree(bam *[256]byte) (track, sector uint8, ok bool) {
	for distance := uint8(1); distance <= 18; distance++ {
		for _, t := range [2]int{dirTrack - int(distance), dirTrack + int(distance)} {
			if t < 1 || t > 35 {
				continue
			}
			track := uint8(t)
			for s := 0; s < sectorsPerTrack(track); s++ {
				if bamIsFree(bam, track, uint8(s)) {
					return track, uint8(s), true
				}
			}
		}
	}
	return 0, 0, false
}

// diskName returns the 16-byte disk name from the BAM sector, trimmed of
// its $A0 padding.
func diskName() string {
	bam := diskReadSector(dirTrack, 0)
	if bam == nil {
		return ""
	}
	n := 0x90
	for n < 0xA0 && bam[n] != 0xA0 && bam[n] != 0x00 {
		n++
	}
	return string(bam[0x90:n])
}

// diskWriteFile writes data as a new file, allocating blocks from the BAM
// and adding a directory entry. It reports a CBM DOS error code: 0 on
// success, 72 when the disk is full, 26 when it is write protected (which
// here means "no disk").
func diskWriteFile(name string, typ uint8, data []byte) int {
	if diskImage == nil {
		return 74 // DRIVE NOT READY
	}
	bam := diskReadSector(dirTrack, 0)
	if bam == nil {
		return 74
	}

	// Lay out the chain first, so a disk-full failure leaves the BAM and
	// the directory untouched rather than half-written.
	blocks := (len(data) + sectorPayload - 1) / sectorPayload
	if blocks == 0 {
		blocks = 1 // even an empty file occupies one block
	}
	type chainLink struct{ track, sector uint8 }
	chain := make([]chainLink, 0, blocks)
	for i := 0; i < blocks; i++ {
		t, s, ok := bamFindFree(bam)
		if !ok {
			return 72 // DISK FULL
		}
		bamAllocate(bam, t, s)
		chain = append(chain, chainLink{t, s})
	}

	slotTrack, slotSector, slotIndex, ok := dirFindSlot(bam)
	if !ok {
		return 72
	}

	// Now commit: the data blocks, then the directory slot, then the BAM.
	for i, link := range chain {
		var block [256]byte
		start := i * sectorPayload
		end := start + sectorPayload
		if end > len(data) {
			end = len(data)
		}
		copy(block[2:], data[start:end])
		if i+1 < len(chain) {
			block[0], block[1] = chain[i+1].track, chain[i+1].sector
		} else {
			// Last block: track zero, and the sector byte holds the
			// offset of the last byte in use.
			block[0], block[1] = 0, uint8(end-start+1)
		}
		diskWriteSector(link.track, link.sector, block[:])
	}

	slot := diskReadSector(slotTrack, slotSector)
	if slot == nil {
		return 74
	}
	off := slotIndex * dirEntrySize
	for i := 2; i < dirEntrySize; i++ {
		slot[off+i] = 0
	}
	slot[off+2] = ftypeClosed | typ
	slot[off+3] = chain[0].track
	slot[off+4] = chain[0].sector
	for i := 0; i < 16; i++ {
		if i < len(name) {
			slot[off+5+i] = name[i]
		} else {
			slot[off+5+i] = 0xA0
		}
	}
	slot[off+30] = uint8(len(chain))
	slot[off+31] = uint8(len(chain) >> 8)
	diskWriteSector(slotTrack, slotSector, slot[:])
	diskWriteSector(dirTrack, 0, bam[:])
	return 0
}

// dirFindSlot returns a free directory slot, extending the directory
// chain by one sector if every existing slot is taken. bam is updated in
// memory when a new directory sector is allocated; the caller writes it
// back.
func dirFindSlot(bam *[256]byte) (track, sector uint8, index int, ok bool) {
	t, s := uint8(dirTrack), uint8(dirFirstSec)
	for visited := 0; visited < 1000; visited++ {
		block := diskReadSector(t, s)
		if block == nil {
			return 0, 0, 0, false
		}
		for i := 0; i < dirPerSector; i++ {
			if block[i*dirEntrySize+2] == 0 {
				return t, s, i, true
			}
		}
		if block[0] == 0 {
			// End of the chain and no room: append a new directory
			// sector on track 18.
			for candidate := 0; candidate < sectorsPerTrack(dirTrack); candidate++ {
				if !bamIsFree(bam, dirTrack, uint8(candidate)) {
					continue
				}
				bamAllocate(bam, dirTrack, uint8(candidate))
				var fresh [256]byte
				fresh[1] = 0xFF
				diskWriteSector(dirTrack, uint8(candidate), fresh[:])
				block[0], block[1] = dirTrack, uint8(candidate)
				diskWriteSector(t, s, block[:])
				return dirTrack, uint8(candidate), 0, true
			}
			return 0, 0, 0, false
		}
		t, s = block[0], block[1]
	}
	return 0, 0, 0, false
}

// diskScratch deletes every file matching pattern, freeing its blocks,
// and returns how many it removed.
func diskScratch(pattern string) int {
	bam := diskReadSector(dirTrack, 0)
	if bam == nil {
		return 0
	}
	removed := 0
	for _, e := range diskDirectory() {
		if e.fileType() == ftypeDEL || !cbmMatch(pattern, e.nameString()) {
			continue
		}
		// Free the file's chain, then blank its type byte, which is what
		// marks the slot free.
		track, sector := e.track, e.sector
		for visited := 0; track != 0 && visited < 1000; visited++ {
			block := diskReadSector(track, sector)
			if block == nil {
				break
			}
			bamRelease(bam, track, sector)
			track, sector = block[0], block[1]
		}
		slot := diskReadSector(e.slotTrack, e.slotSector)
		if slot != nil {
			slot[e.slotIndex*dirEntrySize+2] = 0
			diskWriteSector(e.slotTrack, e.slotSector, slot[:])
		}
		removed++
	}
	diskWriteSector(dirTrack, 0, bam[:])
	return removed
}

// FormatDisk returns a freshly formatted 35-track D64 image: an empty
// directory, a BAM with every sector free except the BAM and the first
// directory sector, and the given name and two-character ID.
func FormatDisk(name, id string) []byte {
	image := make([]byte, 174848)
	saved := diskImage
	diskImage = image
	defer func() { diskImage = saved }()

	var bam [256]byte
	bam[0], bam[1] = dirTrack, dirFirstSec // link to the first directory sector
	bam[2] = 0x41                          // DOS version 'A'
	for t := uint8(1); t <= 35; t++ {
		n := sectorsPerTrack(t)
		off := bamOffset(t)
		bam[off] = uint8(n)
		for s := 0; s < n; s++ {
			bam[off+1+s/8] |= 1 << (s % 8)
		}
	}
	padded := func(dst []byte, s string, pad byte) {
		for i := range dst {
			if i < len(s) {
				dst[i] = s[i]
			} else {
				dst[i] = pad
			}
		}
	}
	padded(bam[0x90:0xA0], name, 0xA0) // disk name
	bam[0xA0], bam[0xA1] = 0xA0, 0xA0
	padded(bam[0xA2:0xA4], id, 0xA0) // disk ID
	bam[0xA4] = 0xA0
	bam[0xA5], bam[0xA6] = '2', 'A' // DOS type
	bam[0xA7], bam[0xA8], bam[0xA9], bam[0xAA] = 0xA0, 0xA0, 0xA0, 0xA0

	bamAllocate(&bam, dirTrack, 0)
	bamAllocate(&bam, dirTrack, dirFirstSec)
	diskWriteSector(dirTrack, 0, bam[:])

	var dir [256]byte
	dir[1] = 0xFF // no next sector; every byte of this one is in use
	diskWriteSector(dirTrack, dirFirstSec, dir[:])

	return image
}

// ReadDiskOrPRG loads a D64 disk image file or a PRG file from path.
// If path points to a PRG file (or any file other than a 174848-byte D64 image),
// it creates a formatted 35-track D64 image containing the PRG file.
func ReadDiskOrPRG(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) == D64Size {
		return data, nil
	}
	if strings.EqualFold(filepath.Ext(path), ".prg") || len(data) < D64Size {
		return MakeD64FromPRG(path, data)
	}
	return nil, fmt.Errorf("%s is %d bytes, not a %d byte 35-track D64 or PRG file", path, len(data), D64Size)
}

// MakeD64FromPRG creates a formatted 35-track D64 disk image containing
// the given PRG file data. filename is cleaned to a CBM DOS name (up to 16
// characters).
func MakeD64FromPRG(filename string, prgData []byte) ([]byte, error) {
	cbmName := cleanCBMFilename(filename)
	disk := FormatDisk("TINY64", "2A")

	saved := diskImage
	diskImage = disk
	defer func() { diskImage = saved }()

	errCode := diskWriteFile(cbmName, ftypePRG, prgData)
	if errCode != 0 {
		return nil, fmt.Errorf("failed to write PRG file %q to disk image: CBM DOS error %d", cbmName, errCode)
	}

	return disk, nil
}

func cleanCBMFilename(filename string) string {
	base := filepath.Base(filename)
	ext := filepath.Ext(base)
	if strings.EqualFold(ext, ".prg") {
		base = base[:len(base)-len(ext)]
	}
	base = strings.ToUpper(base)
	if len(base) > 16 {
		base = base[:16]
	}
	if base == "" {
		base = "PROGRAM"
	}
	return base
}

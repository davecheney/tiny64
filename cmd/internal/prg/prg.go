// Package prg reads and decodes Commodore 64 .prg files.
//
// A .prg file is a two byte little endian load address followed by the raw
// bytes to be copied to memory starting at that address. Programs saved by
// BASIC load at $0801 and hold a tokenised BASIC program; machine code
// programs use whatever load address the author chose.
package prg

import (
	"errors"
	"fmt"
	"io"
)

// File is a decoded .prg file: its load address and the payload that follows.
type File struct {
	LoadAddr uint16
	Data     []byte
}

// ErrShort reports a file too small to contain a load address.
var ErrShort = errors.New("prg: file too short: need at least 2 bytes for the load address")

// Parse reads a .prg file from r.
func Parse(r io.Reader) (*File, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("prg: read: %w", err)
	}
	return ParseBytes(b)
}

// ParseBytes decodes a .prg file held in b. The returned File aliases b.
func ParseBytes(b []byte) (*File, error) {
	if len(b) < 2 {
		return nil, ErrShort
	}
	return &File{
		LoadAddr: uint16(b[0]) | uint16(b[1])<<8,
		Data:     b[2:],
	}, nil
}

// Size returns the number of payload bytes, excluding the load address.
func (f *File) Size() int { return len(f.Data) }

// EndAddr returns the address of the final payload byte. Wrap reports whether
// the payload runs past $FFFF, in which case the address has wrapped around.
func (f *File) EndAddr() (addr uint16, wrap bool) {
	if len(f.Data) == 0 {
		return f.LoadAddr, false
	}
	end := int(f.LoadAddr) + len(f.Data) - 1
	return uint16(end), end > 0xFFFF
}

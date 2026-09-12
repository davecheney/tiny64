package prg

import (
	"fmt"
	"io"
)

// Dump writes a hex dump of data to w, labelled with the C64 addresses the
// bytes occupy when loaded at addr. Each line shows sixteen bytes and the
// printable PETSCII characters among them.
func Dump(w io.Writer, addr uint16, data []byte) error {
	for off := 0; off < len(data); off += 16 {
		end := min(off+16, len(data))
		row := data[off:end]
		line := fmt.Sprintf("$%04X  ", addr+uint16(off))
		for i := 0; i < 16; i++ {
			if i < len(row) {
				line += fmt.Sprintf("%02X ", row[i])
			} else {
				line += "   "
			}
			if i == 7 {
				line += " "
			}
		}
		line += " |"
		for _, c := range row {
			if c >= 0x20 && c <= 0x5E {
				line += string(rune(c))
			} else {
				line += "."
			}
		}
		line += "|\n"
		if _, err := io.WriteString(w, line); err != nil {
			return err
		}
	}
	return nil
}

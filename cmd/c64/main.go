// Command c64 runs the emulator in a desktop window.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/desktop"
)

func main() {
	disk := flag.String("disk", "", "insert this D64 disk image into drive 8")
	flag.Parse()

	// Read the disk before opening a window, so a bad path is an error on
	// the command line rather than a window that appears and vanishes.
	var image []byte
	if *disk != "" {
		var err error
		if image, err = readD64(*disk); err != nil {
			log.Fatal(err)
		}
	}

	if err := desktop.Run("c64", func() {
		if image != nil {
			// Inserting a disk plugs a 1541 into the serial bus, if there
			// wasn't one there already.
			tiny64.InsertDisk(image)
		}
	}); err != nil {
		log.Fatal(err)
	}
}

// readD64 loads a disk image, checking it is the size the drive expects.
// Anything else - a .d81, a .g64, a truncated download - would otherwise
// only show up much later as the drive failing to read a track.
func readD64(path string) ([]byte, error) {
	image, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(image) != tiny64.D64Size {
		return nil, fmt.Errorf("%s is %d bytes, not a %d byte 35-track D64", path, len(image), tiny64.D64Size)
	}
	return image, nil
}

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
	drive := flag.String("drive", "1541", "drive to answer for device 8: \"1541\" emulates the drive's CPU and GCR, \"virtual\" implements CBM DOS directly")
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
	// Check the drive name here for the same reason, rather than inside
	// the callback where it would be that disappearing window again.
	if *drive != "1541" && *drive != "virtual" {
		log.Fatalf("unknown -drive %q, want \"1541\" or \"virtual\"", *drive)
	}

	if err := desktop.Run("c64", func() {
		if image != nil {
			// Inserting a disk plugs a 1541 into the serial bus, if there
			// wasn't one there already.
			tiny64.InsertDisk(image)

			if *drive == "virtual" {
				// Only one device can answer for address 8, so the 1541
				// InsertDisk just plugged in has to come back out.
				tiny64.AttachDrive(false)
				tiny64.AttachVirtualDrive(8)
			}
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

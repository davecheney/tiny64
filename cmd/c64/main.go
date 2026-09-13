// Command c64 runs the emulator in a desktop window.
package main

import (
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/desktop"
)

func main() {
	disk := flag.String("disk", "", "insert this D64 disk image or PRG file into drive 8")
	prg := flag.String("prg", "", "insert this PRG file into drive 8 (formatted on a virtual disk)")
	drive := flag.String("drive", "1541", "drive to answer for device 8: \"1541\" emulates the drive's CPU and GCR, \"virtual\" implements CBM DOS directly")
	wedge := flag.Bool("wedge", false, "enable the resident DOS wedge at the BASIC prompt")
	flag.Parse()

	targetFile := *disk
	if targetFile == "" {
		targetFile = *prg
	}

	// Read the disk before opening a window, so a bad path is an error on
	// the command line rather than a window that appears and vanishes.
	var image []byte
	if targetFile != "" {
		var err error
		if image, err = tiny64.ReadDiskOrPRG(os.DirFS(filepath.Dir(targetFile)), filepath.Base(targetFile)); err != nil {
			log.Fatal(err)
		}
	}
	// Check the drive name here for the same reason, rather than inside
	// the callback where it would be that disappearing window again.
	if *drive != "1541" && *drive != "virtual" {
		log.Fatalf("unknown -drive %q, want \"1541\" or \"virtual\"", *drive)
	}

	if err := desktop.Run("c64", func() {
		if *wedge {
			tiny64.EnableDOSWedge()
		}
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

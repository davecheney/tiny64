// Command c64 runs the emulator in a desktop window.
package main

import (
	"flag"
	"log"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/desktop"
)

func main() {
	disk := flag.String("disk", "", "insert this D64 disk image or PRG file into drive 8")
	prg := flag.String("prg", "", "insert this PRG file into drive 8 (formatted on a virtual disk)")
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
		if image, err = tiny64.ReadDiskOrPRG(targetFile); err != nil {
			log.Fatal(err)
		}
	}

	if err := desktop.Run("c64", func() {
		if *wedge {
			tiny64.EnableDOSWedge()
		}
		if image != nil {
			// Inserting a disk plugs the virtual drive into the serial
			// bus, if there wasn't one there already.
			tiny64.InsertDisk(image)
		}
	}); err != nil {
		log.Fatal(err)
	}
}

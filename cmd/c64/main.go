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
	autostart := flag.String("autostart", "", `insert this D64 or PRG and run it: LOAD"*",8,1 then RUN`)
	flag.Parse()

	// -disk, -prg and -autostart all name one image for drive 8; they
	// differ only in what happens once it is in there, so exactly one of
	// them may be given and whichever it is names the file.
	var targetFile string
	named := 0
	for _, f := range []string{*disk, *prg, *autostart} {
		if f != "" {
			targetFile = f
			named++
		}
	}
	if named > 1 {
		log.Fatal("specify at most one of -disk, -prg or -autostart")
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

	// The prelude runs the machine to the BASIC prompt and types the load
	// itself, so it has to happen after reset and before the window's
	// frame loop takes over.
	var prelude func()
	if *autostart != "" {
		prelude = tiny64.Autostart
	}

	if err := desktop.Run("c64", func() {
		if image != nil {
			// Inserting a disk plugs the virtual drive into the serial
			// bus, if there wasn't one there already.
			tiny64.InsertDisk(image)
		}
	}, prelude); err != nil {
		log.Fatal(err)
	}
}

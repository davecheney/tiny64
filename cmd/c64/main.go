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

	if err := desktop.Run("c64", func() {
		if *disk != "" {
			if err := insertDisk(*disk); err != nil {
				log.Fatal(err)
			}
		}
	}); err != nil {
		log.Fatal(err)
	}
}

// insertDisk loads a D64 image and puts it in the drive, which plugs a
// 1541 into the serial bus if there wasn't one there already.
func insertDisk(path string) error {
	image, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(image) != tiny64.D64Size {
		return fmt.Errorf("%s is %d bytes, not a %d byte 35-track D64", path, len(image), tiny64.D64Size)
	}
	tiny64.InsertDisk(image)
	return nil
}

package main

import (
	"log"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/desktop"
	"github.com/davecheney/tiny64/rom"
)

func main() {
	// Dead Test cartridge: /GAME low, /EXROM high, 8K EPROM on /ROMH
	// (HIROM, $E000-$FFFF), overriding the KERNAL entirely.
	insertCart := func() {
		tiny64.GetBus().Insert(rom.DeadTest, true, false, true, false)
	}
	if err := desktop.Run("Dead Test", insertCart); err != nil {
		log.Fatal(err)
	}
}

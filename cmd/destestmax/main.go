package main

import (
	"log"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/desktop"
	"github.com/davecheney/tiny64/rom"
)

func main() {
	// DiSTestMAX cartridge: /GAME low, /EXROM high, 8K EPROM on /ROMH
	// ($E000-$FFFF), overriding the KERNAL entirely.
	insertCart := func() {
		tiny64.GetBus().Insert(rom.DiagCart, true, false, true, false)
	}
	if err := desktop.Run("DesTestMAX", insertCart); err != nil {
		log.Fatal(err)
	}
}

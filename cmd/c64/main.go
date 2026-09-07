package main

import (
	"log"

	"github.com/davecheney/tiny64/cmd/internal/desktop"
)

func main() {
	if err := desktop.Run("c64", nil); err != nil {
		log.Fatal(err)
	}
}

package main

import (
	"log"

	"github.com/davecheney/tiny64/desktop"
)

func main() {
	if err := desktop.Run("tiny64", nil); err != nil {
		log.Fatal(err)
	}
}

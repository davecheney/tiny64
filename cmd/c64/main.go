// Command c64 runs the emulator in a desktop window.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/desktop"
)

func main() {
	disk := flag.String("disk", "", "insert this D64 disk image or PRG file into drive 8")
	prg := flag.String("prg", "", "insert this PRG file into drive 8 (formatted on a virtual disk)")
	wedge := flag.Bool("wedge", false, "enable the resident DOS wedge at the BASIC prompt")
	profileDir := flag.String("pprof", "", "write a CPU profile of each interval into this directory")
	profileEvery := flag.Duration("pprof-every", 5*time.Second, "how much of the run each -pprof profile covers")
	flag.Parse()

	if *profileDir != "" {
		go profileIntervals(*profileDir, *profileEvery)
	}

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
	if err := desktop.Run("c64", func() {
		if *wedge {
			tiny64.EnableDOSWedge()
		}
		if image != nil {
			// Inserting a disk plugs a drive into the serial bus, if
			// there wasn't one there already. Which kind it is was
			// decided at compile time: the virtual drive by default, the
			// 1541 under -tags drive1541.
			tiny64.InsertDisk(image)
		}
	}); err != nil {
		log.Fatal(err)
	}
}

// profileIntervals writes a CPU profile of every period into dir, one file
// per interval: c64-000.pprof covers the first period, c64-001.pprof the
// second, and so on until the process exits.
//
// One profile over a whole run averages phases that have nothing to do
// with each other - the KERNAL booting, the drive loading a program, and
// the program running - and the last of those is the only one worth
// optimising. Splitting by interval lets each be read on its own: start
// the emulator, note the wall-clock second a phase begins, and read the
// file that covers it.
//
// The interval is wall-clock, so a profile spans whatever the emulator was
// doing then, not a fixed number of frames.
func profileIntervals(dir string, period time.Duration) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Printf("pprof: %v", err)
		return
	}
	for i := 0; ; i++ {
		name := filepath.Join(dir, fmt.Sprintf("c64-%03d.pprof", i))
		f, err := os.Create(name)
		if err != nil {
			log.Printf("pprof: %v", err)
			return
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Printf("pprof: %v", err)
			f.Close()
			return
		}
		time.Sleep(period)
		pprof.StopCPUProfile()
		f.Close()
		log.Printf("pprof: %s covers %s to %s", name,
			(time.Duration(i) * period).Round(time.Second),
			(time.Duration(i+1) * period).Round(time.Second))
	}
}

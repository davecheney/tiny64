package tiny64

import (
	"fmt"
	"sync"

	"github.com/davecheney/tiny64/rom"
)

type kernalMode uint8

const (
	kernalModeStock kernalMode = iota
	kernalModeWedge
)

var (
	activeKernalMode = kernalModeStock
	wedgeKernalROM   []byte
	wedgeKernalOnce  sync.Once
)

func setKernalMode(mode kernalMode) {
	activeKernalMode = mode
}

// SetKernalMode selects the active KERNAL profile. Supported values are
// "stock" and "wedge".
func SetKernalMode(mode string) error {
	switch mode {
	case "stock":
		DisableDOSWedge()
	case "wedge":
		EnableDOSWedge()
	default:
		return fmt.Errorf("unknown -kernal %q, want \"stock\" or \"wedge\"", mode)
	}
	return nil
}

func activeKernalROM() []byte {
	if activeKernalMode != kernalModeWedge {
		return rom.Kernal
	}
	wedgeKernalOnce.Do(func() {
		wedgeKernalROM = append([]byte(nil), rom.Kernal...)
	})
	return wedgeKernalROM
}

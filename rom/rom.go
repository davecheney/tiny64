package rom

import (
	_ "embed"
	"unsafe"
)

//go:embed basic.901226-01.bin
var Basic []byte

//go:embed characters.901225-01.bin
var Character []byte

//go:embed kernal.901227-03.bin
var Kernal []byte

//go:embed destest-max.rom
var DiagCart []byte

//go:embed dead_test.bin
var DeadTest []byte

// The wedge image is embedded as a string so that it lands in flash: a
// []byte global is writable, and handing one to the expansion port hides
// from the compiler that nothing ever stores through it, which leaves the
// image sitting in RAM. Cartridge ROM is read-only, as the real chip is.
//
//go:embed doswedge.bin
var dosWedgeImage string

// DOSWedge is the tiny64 DOS wedge cartridge EPROM image, assembled by
// the builder in the tiny64 package's tests and regenerated with
// "go test github.com/davecheney/tiny64 -run TestDOSWedgeEmbeddedImage -update".
// It aliases flash and must not be written.
var DOSWedge = unsafe.Slice(unsafe.StringData(dosWedgeImage), len(dosWedgeImage))

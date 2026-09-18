package rom

import _ "embed"

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

// DOSWedge is the tiny64 DOS wedge cartridge EPROM image, assembled by
// the builder in the tiny64 package's tests and regenerated with
// "go test github.com/davecheney/tiny64 -run TestDOSWedgeEmbeddedImage -update".
//
//go:embed doswedge.bin
var DOSWedge []byte

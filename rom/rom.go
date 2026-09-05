package rom

import _ "embed"

//go:embed basic.901226-01.bin
var Basic []byte

//go:embed characters.901225-01.bin
var Character []byte

//go:embed kernal.901227-03.bin
var Kernal []byte

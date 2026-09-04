package tiny64

import _ "embed"

//go:embed roms/basic.901226-01.bin
var BasicROM []byte

//go:embed roms/characters.901225-01.bin
var CharacterROM []byte

//go:embed roms/kernal.901227-03.bin
var KernalROM []byte

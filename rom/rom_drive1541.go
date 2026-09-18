//go:build drive1541

package rom

import _ "embed"

// Drive1541 is the 1541's 16K DOS ROM. It is only embedded when the
// physical drive is compiled in: a build that answers device 8 with the
// virtual drive has no 6502 to run it, and 16K is worth having back on the
// targets where flash is the scarce resource.

//go:embed 1541.901229-02.bin
var Drive1541 []byte

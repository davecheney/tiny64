package tiny64

import (
	"os"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/davecheney/tiny64/rom"
)

// specExampleCRT is the file header and CHIP header the VICE manual
// prints as its worked example of the container, an 8K "Attack Of The
// Mutant Camels" cartridge, transcribed byte for byte from that dump.
//
// It is here because it is the one fixture in this file that tiny64 did
// not encode itself, which makes it the only one that can catch the
// container's line polarity being read upside down. The manual's table
// says exrom/game of 0/1 is an 8K cartridge, and an 8K cartridge asserts
// /EXROM and leaves /GAME floating; those are the two bytes at $18 and
// $19 below.
//
// The ROM payload is not from the real cartridge - the test appends a
// synthetic 8K pattern - so this is a format fixture, not a dump.
var specExampleCRT = []byte{
	0x43, 0x36, 0x34, 0x20, 0x43, 0x41, 0x52, 0x54, 0x52, 0x49, 0x44, 0x47, 0x45, 0x20, 0x20, 0x20,
	0x00, 0x00, 0x00, 0x40, 0x01, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x41, 0x54, 0x54, 0x41, 0x43, 0x4B, 0x20, 0x4F, 0x46, 0x20, 0x54, 0x48, 0x45, 0x20, 0x4D, 0x55,
	0x54, 0x41, 0x4E, 0x54, 0x20, 0x43, 0x41, 0x4D, 0x45, 0x4C, 0x53, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x43, 0x48, 0x49, 0x50, 0x00, 0x00, 0x20, 0x10, 0x00, 0x00, 0x00, 0x00, 0x80, 0x00, 0x20, 0x00,
}

// TestParseCRTSpecExample pins the direction of the container's /EXROM
// and /GAME bytes against the specification's own example. A reading that
// inverted them would make this cartridge a MAX-mode one and map it at
// $E000 instead of $8000.
func TestParseCRTSpecExample(t *testing.T) {
	image := append(append([]byte(nil), specExampleCRT...), make([]byte, 0x2000)...)
	crt, err := ParseCRT(image)
	if err != nil {
		t.Fatalf("ParseCRT: %v", err)
	}
	if crt.Name != "ATTACK OF THE MUTANT CAMELS" {
		t.Errorf("Name = %q", crt.Name)
	}
	if !crt.Exrom || crt.Game {
		t.Errorf("/EXROM %v /GAME %v, want an 8K cartridge: /EXROM asserted, /GAME floating", crt.Exrom, crt.Game)
	}
	if !crt.ROML || crt.ROMH {
		t.Errorf("ROML %v ROMH %v, want the image on /ROML", crt.ROML, crt.ROMH)
	}
	if len(crt.ROM) != 0x2000 {
		t.Errorf("ROM is %d bytes, want %d", len(crt.ROM), 0x2000)
	}

	// The wiring the parser reports has to be the wiring the PLA acts on,
	// or the image is mapped somewhere other than where it was decoded to.
	GetBus().InsertCRT(crt)
	t.Cleanup(func() { GetBus().Remove() })
	if !cartridge.eightK() || cartridge.ultimax() {
		t.Error("inserted cartridge is not an ordinary 8K cartridge")
	}
}

// TestReadCRTUltimax reads the diagnostic cartridges shipped as fixtures
// and checks they come back wired for MAX mode.
//
// testdata/dead_test.crt is a real container, the v2.0.0-beta.1 release
// of https://github.com/stid/kick-c64-dead-test, which makes it a second
// witness to the /EXROM and /GAME polarity: nothing here wrote it, and it
// carries $18=$01 $19=$00 for a MAX-mode cartridge.
// testdata/destest-max.crt is the DiSTestMAX EPROM image in a container
// tiny64 wrote, because that cartridge is distributed as a raw dump.
//
// The assertions are about the wiring, not the cartridge names: a .crt of
// the same cartridge from somewhere else would name it differently while
// being just as valid to drop in here.
func TestReadCRTUltimax(t *testing.T) {
	for _, file := range []string{"dead_test.crt", "destest-max.crt"} {
		t.Run(file, func(t *testing.T) {
			crt, err := ReadCRT(os.DirFS("testdata"), file)
			if err != nil {
				t.Fatalf("ReadCRT: %v", err)
			}
			if crt.Name == "" {
				t.Error("cartridge has no name")
			}
			if !crt.Game || crt.Exrom {
				t.Errorf("/GAME %v /EXROM %v, want MAX mode: /GAME asserted, /EXROM floating", crt.Game, crt.Exrom)
			}
			if !crt.ROMH || crt.ROML {
				t.Errorf("ROMH %v ROML %v, want the image on /ROMH", crt.ROMH, crt.ROML)
			}
			if len(crt.ROM) != 0x2000 {
				t.Errorf("ROM is %d bytes, want %d", len(crt.ROM), 0x2000)
			}
		})
	}
}

// ultimaxCRT builds a MAX-mode container around an 8K image, as the
// fixtures are laid out, for the error cases below to damage.
func ultimaxCRT() []byte {
	image := make([]byte, 0x40+0x10+0x2000)
	copy(image, "C64 CARTRIDGE   ")
	image[0x13] = 0x40 // header length
	image[0x14] = 0x01 // version 1.0
	image[0x18] = 0x01 // /EXROM inactive
	image[0x19] = 0x00 // /GAME active
	copy(image[0x20:], "TEST")
	copy(image[0x40:], "CHIP")
	image[0x46], image[0x47] = 0x20, 0x10 // packet length $2010
	image[0x4C], image[0x4D] = 0xE0, 0x00 // load address $E000
	image[0x4E], image[0x4F] = 0x20, 0x00 // image size $2000
	return image
}

func TestParseCRTErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		image   func() []byte
		wantErr string
	}{
		{"too short", func() []byte { return make([]byte, 0x20) }, "want at least"},
		{"bad signature", func() []byte {
			b := ultimaxCRT()
			copy(b, "NOT A CART      ")
			return b
		}, "does not begin with"},
		{"c128 cartridge", func() []byte {
			b := ultimaxCRT()
			copy(b, "C128 CARTRIDGE  ")
			return b
		}, "C128"},
		{"header length past eof", func() []byte {
			b := ultimaxCRT()
			b[0x11] = 0xFF
			return b
		}, "past the end"},
		{"bank switching hardware", func() []byte {
			b := ultimaxCRT()
			b[0x17] = 3 // Final Cartridge III
			return b
		}, "hardware type 3"},
		{"16k game", func() []byte {
			b := ultimaxCRT()
			b[0x18], b[0x19] = 0, 0
			return b
		}, "not wired for MAX mode"},
		{"ram off", func() []byte {
			b := ultimaxCRT()
			b[0x18], b[0x19] = 1, 1
			return b
		}, "not wired for MAX mode"},
		{"8k lines with romh load address", func() []byte {
			b := ultimaxCRT()
			b[0x18], b[0x19] = 0, 1
			return b
		}, "not wired for MAX mode"},
		{"ultimax lines with roml load address", func() []byte {
			b := ultimaxCRT()
			b[0x4C], b[0x4D] = 0x80, 0x00
			return b
		}, "not wired as an ordinary 8K cartridge"},
		{"undecoded load address", func() []byte {
			b := ultimaxCRT()
			b[0x4C], b[0x4D] = 0xA0, 0x00
			return b
		}, "$A000"},
		{"chip is not rom", func() []byte {
			b := ultimaxCRT()
			b[0x49] = 2 // flash
			return b
		}, "chip type 2"},
		{"banked chip", func() []byte {
			b := ultimaxCRT()
			b[0x4B] = 1
			return b
		}, "bank 1"},
		{"bad chip signature", func() []byte {
			b := ultimaxCRT()
			copy(b[0x40:], "NOPE")
			return b
		}, "signature"},
		{"zero packet length", func() []byte {
			b := ultimaxCRT()
			b[0x46], b[0x47] = 0, 0
			return b
		}, "shorter than its own"},
		{"zero image size", func() []byte {
			b := ultimaxCRT()
			b[0x4E], b[0x4F] = 0, 0
			return b
		}, "holds no ROM"},
		{"4k image", func() []byte {
			b := ultimaxCRT()
			b[0x4E], b[0x4F] = 0x10, 0x00
			return b[:0x40+0x10+0x1000]
		}, "only 8K cartridge windows"},
		{"truncated rom", func() []byte {
			return ultimaxCRT()[:0x40+0x10+0x1000]
		}, "the image ends"},
		{"two chip packets", func() []byte {
			b := ultimaxCRT()
			second := make([]byte, 0x10+0x2000)
			copy(second, "CHIP")
			second[0x06], second[0x07] = 0x20, 0x10
			second[0x0C], second[0x0D] = 0x80, 0x00
			second[0x0E], second[0x0F] = 0x20, 0x00
			return append(b, second...)
		}, "more than one CHIP packet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCRT(tc.image())
			if err == nil {
				t.Fatal("ParseCRT accepted the image")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestParseCRTShortHeaderLength checks the container's documented wrong
// value. Images exist that store $20 where the header length belongs;
// seeking there would read the name field as a CHIP header, so the
// parser treats $40 as a floor the way VICE does.
func TestParseCRTShortHeaderLength(t *testing.T) {
	image := ultimaxCRT()
	image[0x13] = 0x20
	if _, err := ParseCRT(image); err != nil {
		t.Fatalf("ParseCRT: %v", err)
	}
}

func TestReadCRTMissingFile(t *testing.T) {
	if _, err := ReadCRT(fstest.MapFS{}, "nope.crt"); err == nil {
		t.Fatal("ReadCRT accepted a missing file")
	}
}

// TestReadCRTWrapsErrorWithFilename checks that a bad image read through
// ReadCRT says which file was bad.
func TestReadCRTWrapsErrorWithFilename(t *testing.T) {
	fsys := fstest.MapFS{"broken.crt": {Data: make([]byte, 0x2000)}}
	_, err := ReadCRT(fsys, "broken.crt")
	if err == nil {
		t.Fatal("ReadCRT accepted a broken image")
	}
	if !strings.Contains(err.Error(), "broken.crt") {
		t.Errorf("error %q does not name the file", err)
	}
}

// TestCRTUltimaxOverridesKERNAL is the end-to-end check that a cartridge
// decoded from a file is mapped the way the file says. A MAX-mode
// cartridge replaces the KERNAL at $E000-$FFFF, so the machine must take
// its reset vector from the cartridge and begin executing inside it.
//
// This is also the second, independent guard on the container's line
// polarity: read upside down, the Dead Test image would be an ordinary 8K
// cartridge, $E000 would still be the KERNAL, and the CPU would start at
// the KERNAL's own entry instead.
//
// It does not look at the screen. A MAX-mode diagnostic cartridge brings
// up neither BASIC nor the screen editor, and DiSTestMAX moves the
// character base to $3800, so the text helpers do not apply.
func TestCRTUltimaxOverridesKERNAL(t *testing.T) {
	crt, err := ReadCRT(os.DirFS("testdata"), "dead_test.crt")
	if err != nil {
		t.Fatalf("ReadCRT: %v", err)
	}

	// coldStart unplugs the cartridge, and Reset is what latches the
	// vector, so the cartridge goes in between the two rather than
	// through newMachine, which does both at once.
	saveMachine(t)
	coldStart()
	GetBus().InsertCRT(crt)
	Reset()
	m := &machine{t: t}

	if got, want := bus.Load(0xFFFC), crt.ROM[0x1FFC]; got != want {
		t.Errorf("$FFFC = $%02X, want the cartridge's $%02X", got, want)
	}
	if got, want := bus.Load(0xFFFC), rom.Kernal[0x1FFC]; got == want {
		t.Errorf("$FFFC = $%02X, which is the KERNAL's byte: the cartridge is not mapped", got)
	}

	// Taken from the image rather than written down here: these are real
	// cartridge dumps, and a different build of one enters somewhere else.
	entry := uint16(crt.ROM[0x1FFC]) | uint16(crt.ROM[0x1FFD])<<8
	if entry < 0xE000 {
		t.Fatalf("cartridge reset vector $%04X is outside its own $E000-$FFFF window", entry)
	}
	if cpu.PC != entry {
		t.Fatalf("reset to $%04X, want the cartridge's entry at $%04X", cpu.PC, entry)
	}

	// Dead Test clears the border and background almost immediately, so
	// the first write to $D020 proves the cartridge is running, not
	// merely that a vector was fetched through it.
	m.runUntil("the cartridge's write to $D020", 100_000, func() bool {
		return !bus.RW && bus.Address == 0xD020
	})
}

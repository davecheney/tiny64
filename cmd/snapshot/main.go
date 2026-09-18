// Command snapshot runs a C64 program headlessly and writes PNG frame captures.
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/prg"
)

const (
	palFramesPerSecond = 985248 / float64(tiny64.CyclesPerFrame)
	bootFrameLimit     = 400
	keyFrames          = 2
)

var (
	borderBounds = image.Rect(0, 0, tiny64.VisibleDotsPerLine, tiny64.VisibleLines)
	activeBounds = image.Rect(48, 51-tiny64.FirstVisibleLine, 368, 251-tiny64.FirstVisibleLine)
)

type config struct {
	prgPath    string
	diskPath   string
	frames     int
	seconds    float64
	output     string
	outPattern string
	crop       bool
	border     bool
	start      optionalAddress
}

type optionalAddress struct {
	value uint16
	set   bool
}

func (a *optionalAddress) String() string {
	if !a.set {
		return ""
	}
	return fmt.Sprintf("$%04X", a.value)
}

func (a *optionalAddress) Set(s string) error {
	s = strings.TrimSpace(s)
	base := 10
	switch {
	case strings.HasPrefix(s, "$"):
		s, base = s[1:], 16
	case strings.HasPrefix(strings.ToLower(s), "0x"):
		s, base = s[2:], 16
	}
	v, err := strconv.ParseUint(s, base, 16)
	if err != nil {
		return fmt.Errorf("invalid address %q", s)
	}
	a.value, a.set = uint16(v), true
	return nil
}

func main() {
	log.SetFlags(0)
	if err := runCLI(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func runCLI(args []string) error {
	cfg, err := parseFlags(args)
	if err != nil {
		return err
	}
	program, disk, err := loadInput(cfg)
	if err != nil {
		return err
	}
	return render(cfg, program, disk)
}

func parseFlags(args []string) (config, error) {
	var cfg config
	fs := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&cfg.prgPath, "prg", "", "PRG file to load and run")
	fs.StringVar(&cfg.diskPath, "disk", "", "D64 image whose first PRG is loaded and run")
	fs.IntVar(&cfg.frames, "frames", 60, "number of frames to run after starting the program")
	fs.Float64Var(&cfg.seconds, "seconds", 0, "seconds to run after starting the program (overrides -frames)")
	fs.StringVar(&cfg.output, "o", "snapshot.png", "final PNG filename")
	fs.StringVar(&cfg.outPattern, "out-pattern", "", "write every frame using a fmt pattern such as frame_%04d.png")
	fs.BoolVar(&cfg.crop, "crop", false, "capture only the 320x200 active display")
	fs.BoolVar(&cfg.border, "border", false, "capture the full 408x293 visible PAL raster (default)")
	fs.Var(&cfg.start, "start", "entry address for machine code, in decimal, $hex, or 0xhex")
	if err := fs.Parse(args); err != nil {
		return config{}, err
	}
	if fs.NArg() != 0 {
		return config{}, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	if (cfg.prgPath == "") == (cfg.diskPath == "") {
		return config{}, errors.New("specify exactly one of -prg or -disk")
	}
	if cfg.seconds < 0 || math.IsNaN(cfg.seconds) || math.IsInf(cfg.seconds, 0) {
		return config{}, errors.New("-seconds must be a finite non-negative number")
	}
	if cfg.seconds > 0 {
		cfg.frames = int(math.Ceil(cfg.seconds * palFramesPerSecond))
	} else if cfg.frames < 1 {
		return config{}, errors.New("-frames must be at least 1")
	}
	if cfg.crop && cfg.border {
		return config{}, errors.New("-crop and -border are mutually exclusive")
	}
	if cfg.output == "" && cfg.outPattern == "" {
		return config{}, errors.New("specify -o or -out-pattern")
	}
	if cfg.outPattern != "" {
		if cfg.output != "" && cfg.output != "snapshot.png" {
			return config{}, errors.New("-o and -out-pattern are mutually exclusive")
		}
		cfg.output = ""
		if !strings.Contains(cfg.outPattern, "%") {
			return config{}, errors.New("-out-pattern must contain a formatting verb such as %04d")
		}
		if fmt.Sprintf(cfg.outPattern, 1) == fmt.Sprintf(cfg.outPattern, 2) {
			return config{}, errors.New("-out-pattern must format the frame number")
		}
	}
	return cfg, nil
}

func loadInput(cfg config) ([]byte, []byte, error) {
	path := cfg.prgPath
	if path == "" {
		path = cfg.diskPath
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	if cfg.prgPath != "" {
		if _, err := prg.ParseBytes(data); err != nil {
			return nil, nil, fmt.Errorf("%s: %w", path, err)
		}
		disk, err := tiny64.MakeD64FromPRG(filepath.Base(path), data)
		return data, disk, err
	}
	if len(data) != tiny64.D64Size && len(data) != tiny64.D64Size40 {
		return nil, nil, fmt.Errorf("%s is %d bytes, not a 35-track or 40-track D64 image", path, len(data))
	}
	_, files, err := tiny64.D64Directory(data)
	if err != nil {
		return nil, nil, err
	}
	for _, file := range files {
		if file.Type != "PRG" || !file.Closed {
			continue
		}
		program, err := tiny64.D64ReadFile(data, file.Name)
		if err != nil {
			return nil, nil, err
		}
		return program, data, nil
	}
	return nil, nil, fmt.Errorf("%s contains no closed PRG files", path)
}

func render(cfg config, program, disk []byte) error {
	resetMachine(disk)
	if err := waitForReady(bootFrameLimit); err != nil {
		return err
	}

	file, err := prg.ParseBytes(program)
	if err != nil {
		return err
	}
	if int(file.LoadAddr)+len(file.Data) > len(tiny64.Ram()) {
		return fmt.Errorf("PRG payload at $%04X is too large for C64 memory", file.LoadAddr)
	}
	copy(tiny64.Ram()[file.LoadAddr:], file.Data)

	_, isBASIC := prg.DecodeBASIC(file.LoadAddr, file.Data)
	switch {
	case cfg.start.set:
		startAt(cfg.start.value)
	case isBASIC:
		setBASICProgram(file.LoadAddr, uint16(int(file.LoadAddr)+len(file.Data)))
		typeRUN()
	default:
		startAt(file.LoadAddr)
	}

	bounds := borderBounds
	if cfg.crop {
		bounds = activeBounds
	}
	for frame := 1; frame <= cfg.frames; frame++ {
		tiny64.StepFrame()
		if cfg.outPattern != "" {
			if err := writePNG(fmt.Sprintf(cfg.outPattern, frame), capture(tiny64.FrameBufferRGBA(), bounds)); err != nil {
				return err
			}
		}
	}
	if cfg.output != "" {
		return writePNG(cfg.output, capture(tiny64.FrameBufferRGBA(), bounds))
	}
	return nil
}

// resetMachine returns the machine to a cold start with disk in drive 8.
// Which drive answers there was decided at compile time - the virtual
// drive by default, the 1541 under -tags drive1541 - so the only thing to
// do here is take the previous one off the bus before InsertDisk puts a
// fresh one on.
func resetMachine(disk []byte) {
	clear(tiny64.Ram())
	clear(tiny64.ColorRam())
	tiny64.ClearFrameBuffer()
	tiny64.Keys().ReleaseAll()
	tiny64.DetachVirtualDrive()
	tiny64.InsertDisk(disk)
	tiny64.Reset()
}

func waitForReady(limit int) error {
	for range limit {
		tiny64.StepFrame()
		for row := range 25 {
			if strings.HasPrefix(screenLine(row), "READY.") {
				return nil
			}
		}
	}
	var visible []string
	for row := range 25 {
		if line := screenLine(row); line != "" {
			visible = append(visible, line)
		}
	}
	return fmt.Errorf("C64 did not reach the BASIC prompt within %d frames (screen: %q)", limit, visible)
}

func screenLine(row int) string {
	ram := tiny64.Ram()
	var b strings.Builder
	for col := range 40 {
		c := ram[0x0400+row*40+col] & 0x7f
		switch {
		case c <= 0x1f:
			c += 0x40
			b.WriteByte(c)
		case c <= 0x3f:
			b.WriteByte(c)
		default:
			b.WriteByte('?')
		}
	}
	return strings.TrimRight(b.String(), " ")
}

func setBASICProgram(start, end uint16) {
	ram := tiny64.Ram()
	setWord := func(addr uint16, value uint16) {
		ram[addr] = byte(value)
		ram[addr+1] = byte(value >> 8)
	}
	setWord(0x2b, start)
	for _, addr := range []uint16{0x2d, 0x2f, 0x31} {
		setWord(addr, end)
	}
}

func typeRUN() {
	for _, key := range []tiny64.Key{tiny64.KeyR, tiny64.KeyU, tiny64.KeyN, tiny64.KeyReturn} {
		tiny64.Keys().Press(key)
		for range keyFrames {
			tiny64.StepFrame()
		}
		tiny64.Keys().Release(key)
		for range keyFrames {
			tiny64.StepFrame()
		}
	}
}

func startAt(addr uint16) {
	cpu := tiny64.GetCPU()
	cpu.PC = addr
	cpu.TState = 0
	cpu.Opcode = 0
	cpu.Operand = 0
	cpu.Pointer = 0
	cpu.Value = 0
	cpu.Addr2 = 0
}

// capture crops bounds out of src, a whole visible frame in row-major RGBA
// order as returned by tiny64.FrameBufferRGBA.
func capture(src []byte, bounds image.Rectangle) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, bounds.Dx(), bounds.Dy()))
	const stride = tiny64.VisibleDotsPerLine * 4
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		start := y*stride + bounds.Min.X*4
		end := start + bounds.Dx()*4
		copy(dst.Pix[(y-bounds.Min.Y)*dst.Stride:], src[start:end])
	}
	return dst
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

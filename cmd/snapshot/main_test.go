package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/davecheney/tiny64"
)

func TestParseFlags(t *testing.T) {
	cfg, err := parseFlags([]string{"-prg", "demo.prg", "-frames", "12", "-crop", "-start", "$080d", "-o", "frame.png"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.prgPath != "demo.prg" || cfg.frames != 12 || !cfg.crop || !cfg.start.set || cfg.start.value != 0x080d || cfg.output != "frame.png" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestSecondsOverridesFrames(t *testing.T) {
	cfg, err := parseFlags([]string{"-prg", "demo.prg", "-frames", "0", "-seconds", "2"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.frames != 101 {
		t.Fatalf("frames = %d, want 101", cfg.frames)
	}
}

func TestParseFlagsRejectsConflicts(t *testing.T) {
	for _, args := range [][]string{
		{"-prg", "a.prg", "-disk", "a.d64"},
		{"-prg", "a.prg", "-crop", "-border"},
		{"-prg", "a.prg", "-frames", "0"},
		{"-prg", "a.prg", "-drive", "fast"},
		{"-prg", "a.prg", "-out-pattern", "frame.png"},
	} {
		if _, err := parseFlags(args); err == nil {
			t.Errorf("parseFlags(%q) succeeded", args)
		}
	}
}

func TestLoadInputSelectsFirstPRG(t *testing.T) {
	want := []byte{0x00, 0x20, 0x60}
	disk, err := tiny64.MakeD64FromPRG("FIRST.PRG", want)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "demo.d64")
	if err := os.WriteFile(path, disk, 0o600); err != nil {
		t.Fatal(err)
	}
	got, gotDisk, err := loadInput(config{diskPath: path})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || !bytes.Equal(gotDisk, disk) {
		t.Fatalf("loadInput returned program % x and disk length %d", got, len(gotDisk))
	}
}

func TestCaptureBounds(t *testing.T) {
	fb := tiny64.FrameBufferRGBA()
	for y := range tiny64.VisibleLines {
		for x := range tiny64.VisibleDotsPerLine {
			i := (y*tiny64.VisibleDotsPerLine + x) * 4
			fb[i], fb[i+1], fb[i+2], fb[i+3] = byte(x), byte(y), 0xaa, 0xff
		}
	}

	full := capture(borderBounds)
	if got := full.Bounds(); got != imageRect(405, 284) {
		t.Fatalf("border bounds = %v", got)
	}
	cropped := capture(activeBounds)
	if got := cropped.Bounds(); got != imageRect(320, 200) {
		t.Fatalf("crop bounds = %v", got)
	}
	if got := cropped.RGBAAt(0, 0); got != (color.RGBA{48, 35, 0xaa, 0xff}) {
		t.Fatalf("first crop pixel = %#v", got)
	}
}

func TestRenderMachineCode(t *testing.T) {
	program := []byte{
		0x00, 0x20, // load address $2000
		0xa9, 0x02, // LDA #$02
		0x8d, 0x20, 0xd0, // STA $D020
		0x4c, 0x05, 0x20, // JMP $2005
	}
	disk, err := tiny64.MakeD64FromPRG("BORDER.PRG", program)
	if err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(t.TempDir(), "frame.png")
	cfg := config{
		drive:  "virtual",
		frames: 2,
		output: output,
		start:  optionalAddress{value: 0x2000, set: true},
	}
	if err := render(cfg, program, disk); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(output)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if got := img.Bounds(); got != imageRect(405, 284) {
		t.Fatalf("rendered bounds = %v", got)
	}
	if got := color.RGBAModel.Convert(img.At(0, 0)).(color.RGBA); got != (color.RGBA{0x88, 0, 0, 0xff}) {
		t.Fatalf("border pixel = %#v, want red", got)
	}
}

func imageRect(width, height int) image.Rectangle {
	return image.Rect(0, 0, width, height)
}

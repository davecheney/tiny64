package main

import (
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/davecheney/tiny64"
)

func paletteColour(index int) color.RGBA {
	p := tiny64.C64Palette[index]
	return color.RGBA{p[0], p[1], p[2], p[3]}
}

func solidSnapshot(bounds image.Rectangle, index int) *image.RGBA {
	img := image.NewRGBA(bounds)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			img.SetRGBA(x, y, paletteColour(index))
		}
	}
	return img
}

func TestCompareSnapshots(t *testing.T) {
	bounds := snapshotBounds(false)
	for _, tc := range []struct {
		name string
		edit func(*image.RGBA) image.Image
		want string
	}{
		{"identical", func(img *image.RGBA) image.Image { return img }, ""},
		{"one-pixel", func(img *image.RGBA) image.Image {
			img.SetRGBA(404, 283, paletteColour(2))
			return img
		}, "1/115020 pixels differ; first mismatch at (404,283); expected colour index 0, actual colour index 2"},
		{"full-raster", func(*image.RGBA) image.Image {
			return solidSnapshot(bounds, 2)
		}, "115020/115020 pixels differ; first mismatch at (0,0); expected colour index 0, actual colour index 2"},
		{"dimensions", func(*image.RGBA) image.Image {
			return solidSnapshot(snapshotBounds(true), 0)
		}, "dimensions"},
		{"shifted-bounds", func(*image.RGBA) image.Image {
			return solidSnapshot(bounds.Add(image.Pt(1, 0)), 0)
		}, "dimensions"},
		{"unknown-colour", func(img *image.RGBA) image.Image {
			img.SetRGBA(5, 6, color.RGBA{1, 0, 0, 255})
			return img
		}, "unknown colour at (5,6)"},
		{"transparent", func(img *image.RGBA) image.Image {
			img.SetRGBA(5, 6, color.RGBA{})
			return img
		}, "unknown colour at (5,6)"},
		{"16-bit-near-colour", func(*image.RGBA) image.Image {
			img := image.NewRGBA64(bounds)
			for y := range bounds.Dy() {
				for x := range bounds.Dx() {
					img.SetRGBA64(x, y, color.RGBA64{A: 65535})
				}
			}
			img.SetRGBA64(5, 6, color.RGBA64{R: 1, A: 65535})
			return img
		}, "unknown colour at (5,6)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expected := solidSnapshot(bounds, 0)
			actual := tc.edit(solidSnapshot(bounds, 0))
			diff, err := compareSnapshots(expected, actual, bounds)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("comparison error = %v, want %q", err, tc.want)
			}
			if diff.Bounds() != bounds {
				t.Fatalf("diff bounds = %v", diff.Bounds())
			}
			if tc.name == "one-pixel" {
				if got := diff.RGBAAt(404, 283); got != (color.RGBA{255, 0, 255, 255}) {
					t.Fatalf("mismatch diff pixel = %v", got)
				}
				if got := diff.RGBAAt(0, 0); got != (color.RGBA{0, 0, 0, 255}) {
					t.Fatalf("matching diff pixel = %v", got)
				}
			}
		})
	}
}

func TestCompareSnapshotPaletteSemantics(t *testing.T) {
	bounds := snapshotBounds(true)
	expected := solidSnapshot(bounds, 2)
	// PNG palette entry numbering is not the C64's colour numbering.
	actual := image.NewPaletted(bounds, color.Palette{paletteColour(2), paletteColour(0)})
	if _, err := compareSnapshots(expected, actual, bounds); err != nil {
		t.Fatal(err)
	}
	expected.SetRGBA(0, 0, color.RGBA{1, 2, 3, 255})
	actual.Palette[0] = color.RGBA{1, 2, 3, 255}
	if _, err := compareSnapshots(expected, actual, bounds); err == nil || !strings.Contains(err.Error(), "unknown colour at (0,0)") {
		t.Fatalf("identical unknown colours must fail: %v", err)
	}
}

func TestReadSnapshot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	for _, crop := range []bool{false, true} {
		bounds := snapshotBounds(crop)
		if err := writePNG(path, solidSnapshot(bounds, 3)); err != nil {
			t.Fatal(err)
		}
		img, err := readSnapshot(path, bounds)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := compareSnapshots(img, solidSnapshot(bounds, 3), bounds); err != nil {
			t.Fatal(err)
		}
		if _, err := readSnapshot(path, snapshotBounds(!crop)); err == nil || !strings.Contains(err.Error(), "dimensions") {
			t.Fatalf("wrong dimensions: %v", err)
		}
	}
	if err := writePNG(path, solidSnapshot(image.Rect(0, 0, 406, 284), 0)); err != nil {
		t.Fatal(err)
	}
	if _, err := readSnapshot(path, snapshotBounds(false)); err == nil {
		t.Fatal("oversized PNG accepted")
	}
	if err := os.WriteFile(path, []byte("not a PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readSnapshot(path, snapshotBounds(false)); err == nil {
		t.Fatal("invalid PNG accepted")
	}
	if _, err := readSnapshot(filepath.Join(dir, "missing.png"), snapshotBounds(false)); err == nil {
		t.Fatal("missing PNG accepted")
	}
}

func TestLoadManifest(t *testing.T) {
	program := []byte{0x00, 0x20, 0x4c, 0x00, 0x20}
	valid := fmt.Sprintf(`{"version":1,"program":{"file":"demo.prg","sha256":"%x"},"crop":false,"checkpoints":[{"frame":2,"png":"frame.png"}]}`, sha256.Sum256(program))
	for _, tc := range []struct {
		name   string
		change func(string) string
		remove string
		want   string
	}{
		{"valid", nil, "", ""},
		{"start-and-crop", func(s string) string { return strings.Replace(s, `"crop":false`, `"start":"$2000","crop":true`, 1) }, "", ""},
		{"version", func(s string) string { return strings.Replace(s, `"version":1`, `"version":2`, 1) }, "", "version 1"},
		{"missing-crop", func(s string) string { return strings.Replace(s, `"crop":false,`, "", 1) }, "", "explicit crop"},
		{"null-crop", func(s string) string { return strings.Replace(s, `"crop":false`, `"crop":null`, 1) }, "", "explicit crop"},
		{"invalid-start", func(s string) string { return strings.Replace(s, `"crop":false`, `"start":"$10000","crop":false`, 1) }, "", "invalid address"},
		{"empty-checkpoints", func(s string) string { return strings.Replace(s, `[{"frame":2,"png":"frame.png"}]`, `[]`, 1) }, "", "at least one checkpoint"},
		{"zero-frame", func(s string) string { return strings.Replace(s, `"frame":2`, `"frame":0`, 1) }, "", "positive, unique"},
		{"duplicate-frame", func(s string) string { return strings.Replace(s, `}]}`, `},{"frame":2,"png":"other.png"}]}`, 1) }, "", "positive, unique"},
		{"duplicate-png", func(s string) string { return strings.Replace(s, `}]}`, `},{"frame":3,"png":"frame.png"}]}`, 1) }, "", "positive, unique"},
		{"hash-mismatch", func(s string) string {
			return strings.Replace(s, fmt.Sprintf("%x", sha256.Sum256(program)), strings.Repeat("0", 64), 1)
		}, "", "sha256 mismatch"},
		{"program-traversal", func(s string) string { return strings.Replace(s, `"demo.prg"`, `"../demo.prg"`, 1) }, "", "invalid .prg filename"},
		{"png-traversal", func(s string) string { return strings.Replace(s, `"frame.png"`, `"../frame.png"`, 1) }, "", "invalid .png filename"},
		{"unknown-field", func(s string) string { return strings.Replace(s, `"version":1`, `"version":1,"typo":true`, 1) }, "", "unknown field"},
		{"trailing-json", func(s string) string { return s + "{}" }, "", "exactly one JSON object"},
		{"invalid-json", func(string) string { return "{" }, "", "unexpected EOF"},
		{"missing-program", nil, "demo.prg", "demo.prg"},
		{"missing-png", nil, "frame.png", "frame.png"},
		{"missing-manifest", nil, "manifest.json", "manifest.json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "demo")
			if err := os.Mkdir(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			manifest := valid
			if tc.change != nil {
				manifest = tc.change(manifest)
			}
			for name, data := range map[string][]byte{
				"manifest.json": []byte(manifest),
				"demo.prg":      program,
				"frame.png":     {},
			} {
				if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.remove != "" {
				if err := os.Remove(filepath.Join(dir, tc.remove)); err != nil {
					t.Fatal(err)
				}
			}
			_, err := loadManifest(dir)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("loadManifest error = %v, want %q", err, tc.want)
			}
		})
	}
	if _, err := loadManifest("Invalid_slug"); err == nil || !strings.Contains(err.Error(), "invalid fixture slug") {
		t.Fatalf("invalid slug: %v", err)
	}
}

func TestWriteFailureImages(t *testing.T) {
	dir := t.TempDir()
	bounds := snapshotBounds(false)
	expected, actual := solidSnapshot(bounds, 0), solidSnapshot(bounds, 2)
	expectedPath, actualPath := filepath.Join(dir, "expected.png"), filepath.Join(dir, "actual.png")
	if err := writePNG(expectedPath, expected); err != nil {
		t.Fatal(err)
	}
	if err := writePNG(actualPath, actual); err != nil {
		t.Fatal(err)
	}
	diff, err := compareSnapshots(expected, actual, bounds)
	if err == nil {
		t.Fatal("expected comparison failure")
	}
	artifacts := filepath.Join(dir, "artifacts", "demo", "frame-000002")
	if err := writeFailureImages(artifacts, expectedPath, actualPath, diff); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]color.RGBA{
		"expected.png": paletteColour(0),
		"actual.png":   paletteColour(2),
		"diff.png":     {255, 0, 255, 255},
	} {
		img, err := readSnapshot(filepath.Join(artifacts, name), bounds)
		if err != nil {
			t.Fatal(err)
		}
		if got := color.RGBAModel.Convert(img.At(0, 0)); got != want {
			t.Errorf("%s: pixel = %v, want %v", name, got, want)
		}
	}

}

func TestManifestRejectsInvalidProgram(t *testing.T) {
	for _, program := range [][]byte{
		{0x00},
		{0xff, 0xff, 0x00, 0x00},
	} {
		dir := filepath.Join(t.TempDir(), "invalid-program")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		manifest := fmt.Sprintf(`{"version":1,"program":{"file":"demo.prg","sha256":"%x"},"crop":false,"checkpoints":[{"frame":2,"png":"frame.png"}]}`, sha256.Sum256(program))
		for name, data := range map[string][]byte{
			"manifest.json": []byte(manifest),
			"demo.prg":      program,
			"frame.png":     {},
		} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := loadManifest(dir); err == nil {
				t.Fatalf("invalid PRG %x accepted", program)
			}
		}
	}
}

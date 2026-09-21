package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"github.com/davecheney/tiny64"
	"github.com/davecheney/tiny64/cmd/internal/prg"
)

type snapshotManifest struct {
	Version int `json:"version"`
	Program struct {
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
	} `json:"program"`
	Start string `json:"start,omitempty"`
	// Uses names the VIC-II features the fixture's picture depends on, so
	// a build that leaves one out can say so rather than fail on every
	// pixel. Empty means a character-mode screen, which every build can
	// render. See vicDrawsEverything.
	Uses        []string             `json:"uses,omitempty"`
	Crop        *bool                `json:"crop"`
	Checkpoints []snapshotCheckpoint `json:"checkpoints"`
}

// unsupportedFeature names the first feature a fixture needs that this
// build does not have, or "" if it can render the fixture.
//
// The names in "uses" are not interpreted. There is one subset today and
// it draws a character-mode screen and nothing else, so a build either
// renders every fixture or only the ones asking for nothing at all. What
// the names are for is saying which feature the skip was about.
func unsupportedFeature(m snapshotManifest) string {
	if vicDrawsEverything || len(m.Uses) == 0 {
		return ""
	}
	return m.Uses[0]
}

type snapshotCheckpoint struct {
	Frame int    `json:"frame"`
	PNG   string `json:"png"`
}

var fixtureSlug = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

func fixtureFile(dir, name, extension string) (string, error) {
	if name == "" || filepath.Base(name) != name || filepath.Ext(name) != extension {
		return "", fmt.Errorf("invalid %s filename %q (want a basename)", extension, name)
	}
	path := filepath.Join(dir, name)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a regular file", path)
	}
	return path, nil
}

func loadManifest(dir string) (snapshotManifest, error) {
	var m snapshotManifest
	if !fixtureSlug.MatchString(filepath.Base(dir)) {
		return m, fmt.Errorf("invalid fixture slug %q", filepath.Base(dir))
	}
	path, err := fixtureFile(dir, "manifest.json", ".json")
	if err != nil {
		return m, err
	}
	f, err := os.Open(path)
	if err != nil {
		return m, err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return m, fmt.Errorf("%s: %w", path, err)
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return m, fmt.Errorf("%s: expected exactly one JSON object", path)
	}
	if m.Version != 1 || m.Crop == nil || len(m.Checkpoints) == 0 {
		return m, fmt.Errorf("%s: require version 1, explicit crop, and at least one checkpoint", path)
	}
	if m.Start != "" {
		var addr optionalAddress
		if err := addr.Set(m.Start); err != nil {
			return m, err
		}
	}
	programPath, err := fixtureFile(dir, m.Program.File, ".prg")
	if err != nil {
		return m, err
	}
	program, err := os.ReadFile(programPath)
	if err != nil {
		return m, err
	}
	digest := sha256.Sum256(program)
	if len(m.Program.SHA256) != 64 || m.Program.SHA256 != hex.EncodeToString(digest[:]) {
		return m, fmt.Errorf("%s: sha256 mismatch: got %x, manifest says %q", programPath, digest, m.Program.SHA256)
	}
	file, err := prg.ParseBytes(program)
	if err != nil {
		return m, fmt.Errorf("%s: %w", programPath, err)
	}
	if int(file.LoadAddr)+len(file.Data) > 65536 {
		return m, fmt.Errorf("%s: program exceeds C64 memory", programPath)
	}
	frames, names := make(map[int]bool), make(map[string]bool)
	for _, checkpoint := range m.Checkpoints {
		if checkpoint.Frame < 1 || frames[checkpoint.Frame] || names[checkpoint.PNG] {
			return m, fmt.Errorf("%s: checkpoints require positive, unique frames and unique PNG filenames", path)
		}
		if _, err := fixtureFile(dir, checkpoint.PNG, ".png"); err != nil {
			return m, err
		}
		frames[checkpoint.Frame], names[checkpoint.PNG] = true, true
	}
	return m, nil
}

func snapshotBounds(crop bool) image.Rectangle {
	if crop {
		return image.Rect(0, 0, activeBounds.Dx(), activeBounds.Dy())
	}
	return borderBounds
}

// Check dimensions before decoding so malformed goldens cannot allocate an
// arbitrarily large raster. Only the CLI's two output sizes are supported.
func readSnapshot(path string, bounds image.Rectangle) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.Width != bounds.Dx() || cfg.Height != bounds.Dy() {
		return nil, fmt.Errorf("%s: dimensions %dx%d, want %dx%d", path, cfg.Width, cfg.Height, bounds.Dx(), bounds.Dy())
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	img, err := png.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return img, nil
}

func paletteIndex(c color.Color) (int, bool) {
	r, g, b, a := c.RGBA()
	for i, p := range tiny64.C64Palette {
		if r == uint32(p[0])*257 && g == uint32(p[1])*257 && b == uint32(p[2])*257 && a == uint32(p[3])*257 {
			return i, true
		}
	}
	return 0, false
}

// Diff pixels are black for agreement and magenta for disagreement, including
// colours outside the exact C64 palette. There is no RGB distance tolerance.
func compareSnapshots(expected, actual image.Image, bounds image.Rectangle) (*image.RGBA, error) {
	diff := image.NewRGBA(bounds)
	if expected.Bounds() != bounds || actual.Bounds() != bounds {
		return diff, fmt.Errorf("dimensions: expected %v, actual %v, want %v", expected.Bounds(), actual.Bounds(), bounds)
	}
	var count int
	var first image.Point
	var firstColours string
	var unknown string
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			want, wantOK := paletteIndex(expected.At(x, y))
			got, gotOK := paletteIndex(actual.At(x, y))
			mark := color.RGBA{0, 0, 0, 255}
			if !wantOK || !gotOK || want != got {
				if count == 0 {
					first = image.Pt(x, y)
					if wantOK && gotOK {
						firstColours = fmt.Sprintf("; expected colour index %d, actual colour index %d", want, got)
					}
				}
				count++
				mark = color.RGBA{255, 0, 255, 255}
				if unknown == "" && (!wantOK || !gotOK) {
					unknown = fmt.Sprintf("; unknown colour at (%d,%d): expected=%v actual=%v", x, y, expected.At(x, y), actual.At(x, y))
				}
			}
			diff.SetRGBA(x, y, mark)
		}
	}
	if count != 0 {
		return diff, fmt.Errorf("%d/%d pixels differ; first mismatch at (%d,%d)%s%s", count, bounds.Dx()*bounds.Dy(), first.X, first.Y, firstColours, unknown)
	}
	return diff, nil
}

func writeFailureImages(dir, expectedPath, actualPath string, diff image.Image) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for name, path := range map[string]string{"expected.png": expectedPath, "actual.png": actualPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			return err
		}
	}
	return writePNG(filepath.Join(dir, "diff.png"), diff)
}

package main

import (
	"context"
	"flag"
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

var snapshotFixtureRoot = flag.String("snapshot-fixtures", filepath.Join("..", "..", "testdata", "demos"), "directory containing snapshot fixture subdirectories")

// A new test-binary process per checkpoint isolates package-global emulator
// state without recompiling the command for every capture.
func TestSnapshotProcess(t *testing.T) {
	if os.Getenv("TINY64_SNAPSHOT_PROCESS") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			if err := runCLI(os.Args[i+1:]); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("missing snapshot arguments")
}

func TestSnapshotFixtures(t *testing.T) {
	root := *snapshotFixtureRoot
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	fixtures := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		fixtures++
		t.Run(entry.Name(), func(t *testing.T) {
			dir := filepath.Join(root, entry.Name())
			manifest, err := loadManifest(dir)
			if err != nil {
				t.Fatal(err)
			}
			if missing := unsupportedFeature(manifest); missing != "" {
				t.Skipf("fixture needs %s, which this build does not draw", missing)
			}
			for _, checkpoint := range manifest.Checkpoints {
				t.Run(fmt.Sprintf("frame-%06d", checkpoint.Frame), func(t *testing.T) {
					actualPath := filepath.Join(t.TempDir(), "actual.png")
					args := []string{
						"-test.run=^TestSnapshotProcess$", "--",
						"-prg", filepath.Join(dir, manifest.Program.File),
						"-frames", strconv.Itoa(checkpoint.Frame),
						"-o", actualPath,
					}
					if *manifest.Crop {
						args = append(args, "-crop")
					} else {
						args = append(args, "-border")
					}
					if manifest.Start != "" {
						args = append(args, "-start", manifest.Start)
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					cmd := exec.CommandContext(ctx, executable, args...)
					cmd.Env = append(os.Environ(), "TINY64_SNAPSHOT_PROCESS=1")
					if output, err := cmd.CombinedOutput(); err != nil {
						t.Fatalf("snapshot subprocess: %v (context: %v)\n%s", err, ctx.Err(), output)
					}

					expectedPath := filepath.Join(dir, checkpoint.PNG)
					bounds := snapshotBounds(*manifest.Crop)
					expected, expectedErr := readSnapshot(expectedPath, bounds)
					actual, actualErr := readSnapshot(actualPath, bounds)
					diff := image.NewRGBA(bounds)
					var compareErr error
					if expectedErr == nil && actualErr == nil {
						diff, compareErr = compareSnapshots(expected, actual, bounds)
					}
					if expectedErr == nil && actualErr == nil && compareErr == nil {
						return
					}
					artifacts := os.Getenv("SNAPSHOT_ARTIFACT_DIR")
					if artifacts == "" {
						artifacts = t.TempDir()
					}
					artifacts = filepath.Join(artifacts, entry.Name(), fmt.Sprintf("frame-%06d", checkpoint.Frame))
					if err := writeFailureImages(artifacts, expectedPath, actualPath, diff); err != nil {
						t.Errorf("write failure images: %v", err)
					}
					t.Fatalf("snapshot mismatch: expected: %v; actual: %v; comparison: %v\nfailure images: %s", expectedErr, actualErr, compareErr, artifacts)
				})
			}
		})
	}
	if fixtures == 0 {
		t.Fatal("no snapshot fixtures found")
	}
}

package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/davecheney/tiny64"
)

func TestTraceCompletedCycles(t *testing.T) {
	const cycles = tiny64.CyclesPerLine + 1
	if os.Getenv("TINY64_TEST_C64CLI") == "1" {
		flag.CommandLine = flag.NewFlagSet("c64cli", flag.ExitOnError)
		os.Args = []string{"c64cli", "-trace", "-cycles", strconv.Itoa(cycles)}
		main()
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTraceCompletedCycles$")
	cmd.Env = append(os.Environ(), "TINY64_TEST_C64CLI=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("c64cli: %v\n%s", err, out)
	}
	pattern := regexp.MustCompile(`(?m)^\s*(\d+) (PC=.*) \| DOT=\s*(\d+) RASTER=\s*(\d+)$`)
	records := pattern.FindAllStringSubmatch(string(out), -1)
	if len(records) != cycles {
		t.Fatalf("got %d trace records, want %d:\n%s", len(records), cycles, out)
	}
	for i, record := range records {
		n := i + 1
		if record[1] != strconv.Itoa(n) ||
			record[3] != strconv.Itoa(n*tiny64.DotsPerCycle%tiny64.DotsPerLine) ||
			record[4] != strconv.Itoa(n/tiny64.CyclesPerLine) {
			t.Fatalf("trace %d has incorrect cycle or beam position: %s", n, record[0])
		}
	}
	wantInitial := []string{
		"PC=FCE3 OP=A2 T=1 A=00 X=00 Y=00 SP=FD P=04 | ADDR=FCE2 DATA=A2 R",
		"PC=FCE4 OP=A2 T=0 A=00 X=FF Y=00 SP=FD P=84 | ADDR=FCE3 DATA=FF R",
	}
	for i, want := range wantInitial {
		if records[i][2] != want {
			t.Errorf("cycle %d state=%q, want %q", i+1, records[i][2], want)
		}
	}
	finalCPU, _, _ := strings.Cut(records[len(records)-1][2], " | ")
	wantStop := "stopped after " + strconv.Itoa(cycles) + " cycles (0 stalled, 0.0%): " + finalCPU + "\n"
	if !strings.HasSuffix(string(out), wantStop) {
		t.Errorf("missing matching stop summary %q:\n%s", wantStop, out)
	}
}

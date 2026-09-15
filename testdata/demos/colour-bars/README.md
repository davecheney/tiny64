# Colour bars

This original synthetic program was written for tiny64's snapshot tests.
It contains no third-party demo code or assets; its instructions and captures
were created specifically for inclusion in this repository. `generate.go`
contains the complete instruction source.
Its BASIC `10 SYS2061` stub exercises the snapshot command's `RUN` path.
Machine code disables interrupts, selects a blue border and black background,
then fills screen and colour RAM with solid characters cycling through all
16 C64 colours before looping forever.

To reproduce the PRG, run `go run generate.go` from this directory. To
deliberately generate the reference captures, run from the repository root:

```sh
go run ./cmd/snapshot -prg testdata/demos/colour-bars/colour-bars.prg -frames 2 -border -o testdata/demos/colour-bars/frame-000002.png
go run ./cmd/snapshot -prg testdata/demos/colour-bars/colour-bars.prg -frames 10 -border -o testdata/demos/colour-bars/frame-000010.png
```

Check the PRG SHA-256 against `manifest.json` after regenerating. Goldens
must be visually reviewed rather than automatically accepted from test output.

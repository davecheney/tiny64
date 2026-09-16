# Self-Portrait Electric

This fixture caches the PRG for **Self-Portrait Electric**, released on CSDB
and supplied by the repository maintainer for tiny64's snapshot tests. It uses
raster IRQs to interlace the screen, so the fixture keeps two consecutive
settled reference frames to cover both phases of the effect.

- Source: <https://csdb.dk/release/?id=6449>
- Cached program: `self-portrait-electric.prg`
- SHA-256: `6f4c2d420ac09cee66a8b81350cc2abf98d50b0d6aa18e091854703966fd8302`

Capture the committed references from the repository root:

```sh
go run ./cmd/snapshot \
  -prg testdata/demos/self-portrait-electric/self-portrait-electric.prg \
  -frames 159 -border \
  -o testdata/demos/self-portrait-electric/frame-000159.png

go run ./cmd/snapshot \
  -prg testdata/demos/self-portrait-electric/self-portrait-electric.prg \
  -frames 160 -border \
  -o testdata/demos/self-portrait-electric/frame-000160.png
```

The reference PNGs were reviewed before committing. Tests use these committed
assets only and do not download external demo content.

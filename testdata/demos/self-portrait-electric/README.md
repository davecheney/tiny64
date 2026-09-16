# Self-Portrait

This fixture caches the PRG for **Self-Portrait** by Electric of Extend, a
multicolour interlace graphics release shown at Assembly 2002, where it placed
fourth in the mixed graphics competition. The picture is interlaced: raster
IRQs alternate two half-pictures every frame, so the fixture keeps two
consecutive settled reference frames to cover both phases of the effect.

- Author: Electric of Extend
- Released: 4 August 2002
- Source: <https://csdb.dk/release/?id=6449>
- Upstream file: <https://csdb.dk/getinternalfile.php/2239/self-portrait_electric.prg>
- Cached program: `self-portrait-electric.prg`
- SHA-256: `6f4c2d420ac09cee66a8b81350cc2abf98d50b0d6aa18e091854703966fd8302`
- Transformations: none. The cached PRG is byte-identical to the upstream file.

The release is titled *Self-Portrait*; the fixture slug and file name keep the
artist's handle because CSDB publishes the file as `self-portrait_electric.prg`.

The copy here is a cache of the publicly released file, committed so the
snapshot tests run offline instead of scraping CSDB on every run.

The PRG contains a BASIC `SYS 2061` stub. Capture the committed references from
the repository root:

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

# Disintegration

This fixture caches the PRG for **Disintegration** by Extend, a C64 graphics
release (multicolour plus sprites) shown at Transmission64 2025, where it
placed second in the C64 graphics competition.

- Author: Extend — code by Kakka, graphics by Sulevi
- Released: 29 November 2025
- Source: <https://csdb.dk/release/?id=257547>
- Upstream file: <https://csdb.dk/getinternalfile.php/275067/disintegration.prg>
- Cached program: `disintegration.prg`
- SHA-256: `04f264f0f9819e079670fba8b259ef789e889c56c57ff7c507e3b6d88b460fbe`
- Transformations: none. The cached PRG is byte-identical to the upstream file.

The copy here is a cache of the publicly released file, committed so the
snapshot tests run offline instead of scraping CSDB on every run.

The PRG contains a BASIC `SYS 12288` stub. Capture the committed reference from
the repository root:

```sh
go run ./cmd/snapshot \
  -prg testdata/demos/disintegration/disintegration.prg \
  -frames 150 -border \
  -o testdata/demos/disintegration/frame-000150.png
```

The reference PNG was reviewed before committing. Tests use these committed
assets only and do not download from CSDB.

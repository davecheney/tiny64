# High on C64

This fixture caches the PRG for **High on C64** by Extend, a C64 graphics
release (hires plus sprites) shown at Assembly Summer 2026, where it placed
second in the mixed graphics competition.

- Author: Extend — code by Kakka, graphics by Sulevi
- Released: 2 August 2026
- Source: <https://csdb.dk/release/?id=263403>
- Upstream file: <https://csdb.dk/getinternalfile.php/282061/high-on-c64.prg>
- Cached program: `high-on-c64.prg`
- SHA-256: `feb7f4bd009db9d1394caea932c7ff31ae3a3db129edc465e4591dd6f171ae1a`
- Transformations: none. The cached PRG is byte-identical to the upstream file.

The copy here is a cache of the publicly released file, committed so the
snapshot tests run offline instead of scraping CSDB on every run.

The PRG contains a BASIC `SYS 12288` stub. Capture the committed reference from
the repository root:

```sh
go run ./cmd/snapshot \
  -prg testdata/demos/high-on-c64/high-on-c64.prg \
  -frames 150 -border \
  -o testdata/demos/high-on-c64/frame-000150.png
```

The reference PNG was reviewed before committing. Tests use these committed
assets only and do not download from CSDB.

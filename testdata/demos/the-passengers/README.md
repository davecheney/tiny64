# The Passengers

This fixture caches the PRG for **The Passengers** by Pretzel Logic, a C64
graphics release (multicolour plus sprites) shown at Transmission64 2021 Fall
Edition, where it placed second in the C64 graphics competition.

- Author: Pretzel Logic — code by Rico, graphics by Mikael
- Released: 4 December 2021
- Source: <https://csdb.dk/release/?id=211680>
- Upstream file: <https://csdb.dk/getinternalfile.php/221931/the-passengers.prg>
- Cached program: `the-passengers.prg`
- SHA-256: `a7768927b190451386c38046d7f274d3c608b2da9548d5a034b6550bafd18a1a`
- Transformations: none. The cached PRG is byte-identical to the upstream file.

The author granted permission to redistribute this PRG and its snapshot golden
in tiny64's test suite. The copy here is a cache of the publicly released file,
committed so the snapshot tests run offline instead of scraping CSDB on every
run.

The PRG contains a BASIC `SYS 2061` stub. Capture the committed reference from
the repository root:

```sh
go run ./cmd/snapshot \
  -prg testdata/demos/the-passengers/the-passengers.prg \
  -frames 120 -border \
  -o testdata/demos/the-passengers/frame-000120.png
```

The reference PNG was reviewed before committing. Tests use these committed
assets only and do not download from CSDB.

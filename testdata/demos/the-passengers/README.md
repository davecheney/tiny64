# The Passengers

This fixture caches the PRG for **The Passengers** by Pretzel Logic, released
4 December 2021. The author granted permission to redistribute this PRG and
its snapshot golden in tiny64's test suite.

- Source: <https://csdb.dk/release/?id=211680>
- Cached program: `the-passengers.prg`
- SHA-256: `a7768927b190451386c38046d7f274d3c608b2da9548d5a034b6550bafd18a1a`

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

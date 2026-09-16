# High on C64

This fixture caches the PRG for **High on C64**, released on CSDB and supplied
by the repository maintainer for tiny64's snapshot tests.

- Source: <https://csdb.dk/release/?id=263403>
- Cached program: `high-on-c64.prg`
- SHA-256: `feb7f4bd009db9d1394caea932c7ff31ae3a3db129edc465e4591dd6f171ae1a`

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

# Uncle-Angus McFungus

This fixture caches the PRG for **Uncle-Angus McFungus** by Extend, a C64
graphics release (hires plus sprites) shown at KozMOS 2026, where it placed
ninth in the mixed competition.

- Author: Extend — code by Copyfault, graphics by Duce
- Released: 22 March 2026
- Source: <https://csdb.dk/release/?id=260473>
- Upstream file: <https://csdb.dk/getinternalfile.php/278595/uncle-angus_mcfungus.prg>
- Cached program: `uncle-angus-mcfungus.prg`
- SHA-256: `470e39185204de92bbfce533a883c45053cd22ec74877ff913e5ab8ec3998216`
- Transformations: none. The cached PRG is byte-identical to the upstream file.

The slug follows the release title. CSDB publishes the file itself as
`uncle-angus_mcfungus.prg`, spelling the name "Angus"; the upstream URL above
keeps that spelling so it resolves.

The copy here is a cache of the publicly released file, committed so the
snapshot tests run offline instead of scraping CSDB on every run.

The PRG contains a BASIC `SYS 2061` stub. Capture the committed reference from
the repository root:

```sh
go run ./cmd/snapshot \
  -prg testdata/demos/uncle-angus-mcfungus/uncle-angus-mcfungus.prg \
  -frames 150 -border \
  -o testdata/demos/uncle-angus-mcfungus/frame-000150.png
```

The reference PNG was reviewed before committing. Tests use these committed
assets only and do not download from CSDB.

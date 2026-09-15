# Uncle-Agnus McFungus

This fixture caches the PRG for **Uncle-Agnus McFungus** by Extend, released
22 March 2026. The author granted permission to redistribute this PRG and its
snapshot golden in tiny64's test suite.

- Source: <https://csdb.dk/release/?id=260473>
- Cached program: `uncle-agnus-mcfungus.prg`
- SHA-256: `470e39185204de92bbfce533a883c45053cd22ec74877ff913e5ab8ec3998216`

The PRG contains a BASIC `SYS 2061` stub. Capture the committed reference from
the repository root:

```sh
go run ./cmd/snapshot \
  -prg testdata/demos/uncle-agnus-mcfungus/uncle-agnus-mcfungus.prg \
  -frames 150 -border \
  -o testdata/demos/uncle-agnus-mcfungus/frame-000150.png
```

The reference PNG was reviewed before committing. Tests use these committed
assets only and do not download from CSDB.

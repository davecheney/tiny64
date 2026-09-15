# Disintegration

This fixture caches the PRG for **Disintegration**, released on CSDB. The
author granted permission to redistribute this PRG and its snapshot golden in
tiny64's test suite under the same terms as the other cached PRG fixtures.

- Source: <https://csdb.dk/release/?id=257547>
- Cached program: `disintegration.prg`
- SHA-256: `04f264f0f9819e079670fba8b259ef789e889c56c57ff7c507e3b6d88b460fbe`

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

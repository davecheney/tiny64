# tinygo branch log

The `tinygo` branch is a long-running fork of `main`, hosting the
microcontroller device builds (`cmd/tufty2040`, `cmd/gopher-badge64`).
`main` is pushing towards cycle-accurate VIC-II/6502 emulation good enough
to reproduce C64 demos; that work costs more CPU than an in-order
microcontroller running Go can spend per frame. This branch trades some
of that accuracy back for frame time, and does not carry the real 1541
CPU/VIA/GCR drive emulation — only the existing lightweight virtual IEC
drive (`iecdevice.go`/`cbmdos.go`/`d64.go`).

Rule for taking anything from `main`: a backport is only taken if it does
not regress frame time on the Tufty 2040 (measured via the `emulate=`
line each build already prints over serial, average over 50 frames).
Anything that only affects the real 1541 is out of scope entirely, since
that hardware model no longer exists on this branch.

## Branch point

`78e8f0e` "Align dotclock names with cycle phases" — the last commit that
was itself tinygo/hot-path focused, immediately before `main` started its
cycle-accurate VIC-II/6502-opcode push (the commits below).

## Commits examined (`78e8f0e..main`, oldest first)

| commit | title | decision | notes |
|---|---|---|---|
| `fed7887` | Support auto-mounting PRG files into drive 8 | **picked (full)**, commit `77d5947` | Pure `d64fs.go` addition (`ReadDiskOrPRG`, `MakeD64FromPRG`) plus a `-prg` flag on `cmd/c64`/`cmd/c64cli`; drive-agnostic. Hand-resolved only for this branch's already-dropped `-drive` flag. |
| `df32d90` | Add support for 40-track D64 images | **picked (partial)**, commit `2a93049` | Kept `D64Size40`, the 40-track `sectorsPerTrack`/`trackOffset` extension, and `ReadDiskOrPRG`'s 40-track acceptance + tests. Dropped the `1541disk.go`/`cmd/drivec` hunks (both deleted on this branch) and `TestDriveStepping40Track`, which exercised the real drive's VIA head-stepper (`via2StorePRB`/`driveHalfTrack`), also gone here. |
| `e5ba18f` | Add DOS 5.1 wedge commands | **picked (full)**, commit `3abd2aa` | `doswedge.go` itself only touches BASIC RAM/screen and the DOS command channel — no 1541/VIA/GCR dependency, applied unchanged. Hand-resolved `Reset()` (dropped the deleted `ResetDrive()` call), `InsertDisk`/`replaceDisk` (dropped `driveFlushTrack()`), and `saveMachine`'s snapshot (dropped the deleted `driveCPU`/`via1`/`via2`/`driveRAM`/`driveAttached` fields, kept `virtualDriveAttached` + the new `dosWedge` snapshot). `-drive` flag stayed gone; `-wedge` was kept. |
| `4dafb37` | cmd/prg: add a .prg inspector and 6502 disassembler | **picked (full)** | Standalone desktop debugging tool with no import path into either device firmware. Isolated backport passed `go test ./...` and `go build ./cmd/prg`; its README conflict was resolved without restoring the removed real-drive harness. |
| `c26ae8f` | cmd/snapshot: add headless PNG renderer | **rejected** | Direct compatibility check fails: it calls `tiny64.AttachDrive`, absent with the branch's removed real 1541 emulation. Do not backport without a virtual-drive-specific implementation. |
| `e9f8ccc` | Implement VIC-II graphics modes and raster interrupts | **rejected** | Applied cleanly in an isolated worktree; host tests and Tufty build passed, but a physical Tufty 2040 A/B at `-opt=2 -scheduler=none` regressed from a 74.77ms/frame baseline to 91.59-91.63ms/frame over five stable 50-frame windows. Baseline firmware was restored. |
| `ca2f6a2` | Support illegal NOP, LAX, and DCP opcodes in 6510 CPU | **picked (full)** | Adds illegal NOP, LAX, and DCP opcodes to the 6510 CPU core. Applied cleanly. On-device measurement on Tufty 2040 at `-opt=2` confirmed no frame-time degradation (steady `emulate≈74.5ms/frame` across 450+ frames, matching baseline). |
| `21ddeb5` | vic: add unit test for 40-to-38 column sideborder opening trick | **skip** | Isolated cherry-pick conflicts because `vic_modes_test.go` is created by the rejected `e9f8ccc`; test-only and inapplicable without it. |
| `f0470b4` | vic: add unit tests for 38-to-40 border opening and vertical border comparison rules | **skip** | Isolated cherry-pick conflicts because it modifies `vic_modes_test.go`, created by rejected `e9f8ccc`. |
| `e9772f3` | vic: gate pixel output on the main border flip-flop only | **skip** | Isolated cherry-pick conflicts in `6569.go` and `vic_modes_test.go`; depends on rejected `e9f8ccc` and its border state. |
| `3ede1ec` | vic: implement sprites and sprite DMA bus cycle stealing | **skip** | Isolated cherry-pick conflicts in `6569.go` and test files; depends on the rejected graphics-mode/border sequence. |
| `1fb80cd` | vic: latch sprite DMA per line instead of reading registers live | **skip** | Sprite-DMA correctness follow-up; `vic_sprites_test.go` and the required `3ede1ec` sprite base are absent. |
| `49bb855` | vic: pin down the sprite DMA and BA behaviour with tests | **skip** | Test-only follow-up requiring the skipped `3ede1ec` sprite-DMA implementation. |
| `f482449` | build: upgrade Ebitengine to 2.10.1 | **skip** | Ebitengine is used only by the desktop frontend and is absent from both TinyGo device dependency graphs. The update passed isolated desktop and device-build checks, but is not a TinyGo improvement and was reverted to keep this branch hardware-focused. |
| `c19872c` | Fix VIC-II right-edge raster seam | **skip** | Isolated cherry-pick conflicts in `6569.go` and its test; the raster path is part of the rejected graphics-mode/sprite implementation. |
| `e5c0950` | Use fs.FS for D64 loader | **picked (full)** | Refactors D64 filesystem loader to use standard library `io/fs.FS`. Applied cleanly; `go test ./...` and device builds passed. |
| `a0c5bbe` | 6510: implement missing illegal opcodes ANC and LAX family | **picked (full)** | Implements ANC (0x0B/0x2B) and full LAX family (0xA3/0xA7/0xAF/0xB3/0xB7/0xBF) in 6510 CPU core. Applied cleanly; on-device measurement on Tufty 2040 at `-opt=2 -scheduler=none` confirmed no frame-time regression (steady `emulate≈74.67ms/frame` across 10 50-frame windows, matching baseline). |

## Performance investigation: frame-time gap vs main's ~70ms target

On-device (Tufty 2040) frame times after the strip-down and three
backports above sit around emulate≈76-80ms/frame at TinyGo's default
`-opt=z` (optimize for size), vs. main's stated cycle-accurate ~70ms
target on the same class of hardware. Investigated two candidate causes:

- **Scheduler**: `cmd/tufty2040` has no goroutines, so its flash and
  measurement commands use `-scheduler=none`. `cmd/gopher-badge64` has a
  render goroutine and must use `-scheduler=cores`; these target-specific
  settings must not be interchanged. `-scheduler=none` builds cleanly and
  is ~1.3KB smaller on Tufty, though it did not independently improve
  frame time in prior measurements.
- **XIP (execute-in-place flash) cache pressure**: added permanent
  telemetry (`xip=H.HH% (hit/access accesses)` in the per-50-frame serial
  report on both `cmd/tufty2040` and `cmd/gopher-badge64`) reading the
  RP2040's free-running `XIP_CTRL.CTR_HIT`/`CTR_ACC` counters. Measured
  hit rate is **≈99.97-100%** regardless of build (`-opt=z` or `-opt=2`)
  — flash-cache pressure is not the cause of the frame-time gap.
- **`-opt` level**: `-opt=1` (161KB flash) and `-opt=2` (156KB flash)
  both build; `-opt=2` measured emulate≈73-74ms/frame on hardware, a
  real ~7% win over `-opt=z`'s ≈79ms, for ~45KB more flash and no
  measurable RAM cost. XIP hit rate was unchanged at `-opt=2` (still
  ≈99.97-100%), confirming the win comes from better-optimized codegen,
  not reduced cache pressure. **`-opt=2` is kept as the recommended flag**
  going forward (see README); `-opt=1` was not separately re-verified on
  hardware since `-opt=2` is both smaller and, by TinyGo's design intent,
  at least as fast.

Root cause of the remaining ~73ms (opt=2) vs ~70ms gap is still open —
candidates not yet investigated: 6510 dispatch-table efficiency, GC
pressure/allocation pattern, or redundant per-frame work in the VIC-II
raster loop. The XIP and `-opt` telemetry/findings are being kept
permanently (both in code and in this log) since they narrow the search
space for whoever picks this up next.

## Tried and rejected: main PR #24 (VIC-II hoist)/'davecheney-effective-spork' branch

`main`'s open PR #24 (`f11cc3b`, `7716d73`, `8b6a30f`, branch
`dfc/vic-hoist-commits-2-4`) hoists the hblank and per-build render-window
tests out of the per-dot `dotclock1`-`dotclock7` calls in `6569.go`, and
was measured on `main` (Gopher Badge) as a ~1.65% win over `78e8f0e`.
Since this branch's `6569.go`/`vic_test.go` are untouched since the branch
point, all three commits cherry-picked onto `tinygo` cleanly with zero
conflicts, and `go build`/`go test ./...` and both TinyGo device builds
passed.

On-device (Tufty 2040) it measured **worse**, not better: emulate≈76-77ms
at `-opt=2` (previously ≈73-74ms/frame without the hoist, at the same
`-opt=2`), consistent across a 400+ frame capture. XIP hit rate was
unaffected (≈99.97-99.99%), so this was not a cache-pressure change - the
extra per-cycle branching/bookkeeping the hoist adds appears to cost more
on this compiler/target combination than the branches it removes save.
This is the opposite of the (real, PR #24-confirmed) result on the Gopher
Badge, so the two boards/build configurations disagree here.

**Rejected. Reverted from this branch** (the three commits were dropped
via `git reset --hard` back to `43f319b` before ever being pushed, so
`tinygo`'s history has no trace of the attempt beyond this log entry).
Not backported. If revisited, it needs its own A/B on the Tufty 2040 (and
ideally the Gopher Badge too, since this branch targets both) rather than
assuming the Badge result transfers.

## Flashing and Monitoring Workflow (Tufty 2040)

- **Build & Flash**: `tinygo flash -target=tufty2040 -opt=2 -scheduler=none ./cmd/tufty2040` (compiles and flashes in a single step).
- **Bootloader Reset**: Opening USB CDC port at 1200 baud resets the RP2040 into bootloader mode (`/Volumes/RPI-RP2`).
- **Serial Telemetry Monitoring**: Use `tinygo monitor` or read USB serial port (`/dev/cu.usbmodem1201` on macOS) at 115200 baud. Average `emulate=...ms` over 50-frame windows. Baseline is ~73-74ms/frame at `-opt=2 -scheduler=none`.

## Contributing back to main

This branch also contributes performance fixes upstream to `main` when
they are general (not specific to dropping the 1541). None have been
identified/sent yet as of this log entry - watch for opportunities while
doing tinygo-side performance work (e.g. VIC-II or CPU hot-path
simplifications discovered while chasing frame time on-device).

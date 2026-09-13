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
cycle-accurate VIC-II/6502-opcode push (the 9 commits below).

## Commits examined so far (`78e8f0e..main`, oldest first)

| commit | title | decision | notes |
|---|---|---|---|
| `fed7887` | Support auto-mounting PRG files into drive 8 | **picked (full)**, commit `77d5947` | Pure `d64fs.go` addition (`ReadDiskOrPRG`, `MakeD64FromPRG`) plus a `-prg` flag on `cmd/c64`/`cmd/c64cli`; drive-agnostic. Hand-resolved only for this branch's already-dropped `-drive` flag. |
| `df32d90` | Add support for 40-track D64 images | **picked (partial)**, commit `2a93049` | Kept `D64Size40`, the 40-track `sectorsPerTrack`/`trackOffset` extension, and `ReadDiskOrPRG`'s 40-track acceptance + tests. Dropped the `1541disk.go`/`cmd/drivec` hunks (both deleted on this branch) and `TestDriveStepping40Track`, which exercised the real drive's VIA head-stepper (`via2StorePRB`/`driveHalfTrack`), also gone here. |
| `e5ba18f` | Add DOS 5.1 wedge commands | **picked (full)**, commit `3abd2aa` | `doswedge.go` itself only touches BASIC RAM/screen and the DOS command channel — no 1541/VIA/GCR dependency, applied unchanged. Hand-resolved `Reset()` (dropped the deleted `ResetDrive()` call), `InsertDisk`/`replaceDisk` (dropped `driveFlushTrack()`), and `saveMachine`'s snapshot (dropped the deleted `driveCPU`/`via1`/`via2`/`driveRAM`/`driveAttached` fields, kept `virtualDriveAttached` + the new `dosWedge` snapshot). `-drive` flag stayed gone; `-wedge` was kept. |
| `4dafb37` | cmd/prg: add a .prg inspector and 6502 disassembler | **deferred** | New standalone `cmd/prg` tool; not drive/VIC/CPU-timing related, so it should backport cleanly, but not yet reviewed/picked. Low priority - a desktop-only debugging tool. |
| `c26ae8f` | cmd/snapshot: add headless PNG renderer | **deferred** | New standalone `cmd/snapshot` tool. Desktop-only (image/png, no tinygo relevance); low priority, not yet reviewed. |
| `e9f8ccc` | Implement VIC-II graphics modes and raster interrupts | **skip (for now)** | Core of the cycle-accurate VIC-II push. Large, and exactly the kind of change whose frame-time cost needs to be measured before it can be considered - not yet attempted. Revisit only if a specific mode/feature is needed and can be shown not to regress frame time. |
| `ca2f6a2` | Support illegal NOP, LAX, and DCP opcodes in 6510 CPU | **skip (for now)** | Adds illegal-opcode support to the 6510 core the tinygo branch shares with main. Likely cheap (dispatch table additions), but not yet measured; revisit if a specific demo/cartridge needs it. |
| `21ddeb5` | vic: add unit test for 40-to-38 column sideborder opening trick | **skip** | Test-only, for VIC-II behaviour (`e9f8ccc`) not present on this branch. Pointless without that commit. |
| `f0470b4` | vic: add unit tests for 38-to-40 border opening and vertical border comparison rules | **skip** | Same as above - test-only, depends on `e9f8ccc`. |
| `e9772f3` | vic: gate pixel output on the main border flip-flop only | **skip (for now)** | Depends on the VIC-II graphics-mode/border state added in `e9f8ccc`; nothing to backport onto until/unless that lands. |

## Performance investigation: frame-time gap vs main's ~70ms target

On-device (Tufty 2040) frame times after the strip-down and three
backports above sit around emulate≈76-80ms/frame at TinyGo's default
`-opt=z` (optimize for size), vs. main's stated cycle-accurate ~70ms
target on the same class of hardware. Investigated two candidate causes:

- **`-scheduler=none`**: valid on `cmd/tufty2040` (it has no goroutines;
  `cmd/gopher-badge64` does, via its render goroutine, and needs
  `-scheduler=cores`). Builds cleanly, ~1.3KB smaller flash, but no
  measurable frame-time change on hardware. Not adopted as a default
  since it buys nothing measurable and is a footgun if a goroutine is
  ever added to `cmd/tufty2040`.
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

## Contributing back to main

This branch also contributes performance fixes upstream to `main` when
they are general (not specific to dropping the 1541). None have been
identified/sent yet as of this log entry - watch for opportunities while
doing tinygo-side performance work (e.g. VIC-II or CPU hot-path
simplifications discovered while chasing frame time on-device).

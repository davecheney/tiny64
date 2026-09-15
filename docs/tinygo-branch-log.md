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
| `31e8125` | Improve VIC-II border trick timing | **skip** | Requires the rejected graphics-mode path: `advanceGraphicsData`, `paintGraphicsPixel`, and `vic_modes_test.go`. Adds deferred right-border pixel storage and repainting. Patch applicability check fails in `6569.go`, `vic_border_test.go`, and the absent modes test file; not a standalone correction to this branch's renderer. |
| `d3168e1` | Fix VIC-II wrapped sprites and multicolor scroll | **skip** | Changes sprite positioning and multicolor reload timing in the skipped graphics/sprite implementation, plus the right-border state introduced by `31e8125`. Patch check fails; both associated test files are absent. |
| `eb78866` | Test wrapped left-border sprite block | **skip** | Test-only extension of `d3168e1`; `vic_sprites_test.go` and the sprite renderer it exercises are absent. |
| `e864ea1` | Fix same-line VIC raster IRQ triggers | **skip** | Requires `rasterCompare`, `interruptStatus`, `IRQ`, and `checkRasterIRQ` from rejected `e9f8ccc`. This branch does not generate VIC raster IRQs. Patch check fails in the implementation and tests. |
| `d8ec797` | Stabilize raster IRQ timing | **skip** | Both the line-zero VIC comparison change and its CPU test require the skipped raster IRQ implementation. The CPU hunk assumes per-cycle IRQ sampling from skipped `3ede1ec`; this branch samples CIA1 at instruction boundaries. Changing its delay constant alone would not implement the same behavior. Patch check fails. |
| `1c72cf1` | Match raster IRQ recognition timing | **skip** | Revises `d8ec797`'s delay again and uses `VICII.IRQ` in the new CPU test. Neither that test's base nor the per-cycle combined VIC/CIA IRQ sampling exists here. Patch check fails; do not transplant the constant into the different CIA-only path. |
| `6ea675f` | Sample border color writes during Phi2 | **skip** | Repaints the deferred right-border buffer introduced by skipped `31e8125`; that buffer and its edge constants do not exist here. Implementation and test patch checks fail. |
| `0887b36` | desktop: allow the window to be resized | **skip** | Applies cleanly and the isolated desktop build passes with this branch's Ebitengine 2.9.11. `tinygo list -deps` confirms neither device imports Ebitengine or the desktop package. Like `f482449`, this is a desktop-only change rather than a device improvement; no GUI behavior claim was made. |
| `c0ef07e` | pla: map an 8K cartridge's ROML image at $8000-$9FFF | **picked**, commit `1fe666b` | Cartridge prerequisite, accepted with the complete series by explicit approval below. Omitted only the unrelated CIA2 video-bank test dragged into the conflict from rejected `e9f8ccc`; retained both new CPU ROML tests. When added without using a cartridge, its Tufty flash image is byte-identical to baseline because the unused decoder is optimized away. |
| `be1a4e1` | doswedge: give the assembler an origin, drop patchWord | **picked (full)**, commit `c5cb375` | Applied cleanly as preparation for the cartridge. Keep it with the complete series: the intermediate RAM-wedge build has a substantially different flash layout and measured 88.124586ms/frame, unlike the complete series' 74.797776ms/frame. |
| `7093968` | doswedge: deliver the wedge as an 8K autostart cartridge | **picked**, commit `4290e95` | Preserved the removed-drive state when resolving Reset and test snapshot conflicts. Host tests and both device builds pass. The measured full-series cost of 0.08998ms/frame (0.1204%) was explicitly accepted on 2026-09-15; this supersedes the initial rejection below. No experimental decoder reordering was included. |
| `0882c98` | doswedge: test the cartridge boot path | **picked (full)**, commit `686c4e3` | Cartridge tests cover ROM signature, boot vector, free-memory report, workspace, and RESTORE behavior; all pass on the virtual-drive branch. |
| `90081d2` | doc: describe the wedge as the cartridge it now is | **picked**, commit `65cba3d` | Kept the cartridge README and CLI exclusivity guard. Follow-up documentation removes this branch's obsolete `-drive=virtual` example flag and clarifies that DisableDOSWedge immediately unmaps ROM, so Reset must precede further CPU stepping. |
| `3473b18` | tiny64: stop callers assuming the frame buffer is a live RGBA view | **skip** | Preparation for the desktop indexed sink, including migrations in the absent snapshot/sprite code. Its only device-source change adds unused `ClearFrameBuffer`; an isolated Tufty build with that hunk is byte-identical to the accepted baseline, and both device targets build. No device behavior or performance improvement. |
| `5caeefd` | tiny64: add a paletted pixel sink | **skip** | The new sink is explicitly excluded by `tinygo`; device builds retain the cropped RGB565BE buffer. Keep with its desktop presentation series rather than introduce an unused alternative here. |
| `b4e4a33` | desktop: expand palette indices on the GPU | **skip** | Desktop shader/upload path and `.kage` handling, with no import into either firmware target. |
| `5a9e1cb` | doc: describe the paletted frame buffer experiment | **skip** | Documents the skipped desktop experiment and its temporary `paletted` tag. |
| `8177007` | tiny64: make the paletted frame buffer the only one | **skip** | Makes indexing the default for host builds, not TinyGo. Requires the skipped presentation and framebuffer-lifetime migrations. |
| `0ae9df8` | desktop: pad framebuffer row stride to avoid per-frame CPU packing | **skip** | Pads the host indexed buffer to 408 bytes per row for GPU upload. Does not alter the device RGB565BE layout or draw path. |
| `672c3f8` | Consolidate indexed framebuffer storage and verify shader output | **skip** | Consolidates host sinks, corrects tag overlap and documents shared snapshot storage; adds layout and GPU readback tests. TinyGo remains on its existing sink. No complete desktop-series or GPU test run was needed for this device-scope exclusion. |
| `aa710d2` | vic: remove StepDot, leaving stepCycle as the only driver | **picked (adapted)** | Removes per-dot/frame-synchronization entry points, exposes StepCycle and migrates existing callers/tests. Preserves the TinyGo renderer, virtual drive and all retained VIC function bodies; omits absent snapshot, sprite and IRQ surfaces. |
| `98a2b37` | Fix StepCycle review coverage and document API migration | **picked (adapted)** | Retains applicable ordering/beam coverage and README migration guidance. Does not import the sprite test or indexed framebuffer changes absent from this branch. |
| `dceb721` | Pin StepCycle CPU, VIC, IEC and CLI timing contracts | **picked (adapted)** | Includes final CPU/VIC pixel ordering, stalled CPU/CIA clock, same-cycle CIA2/IEC and CLI trace tests, plus corrected comments. Explicitly sets the pixel test's border fixture because this branch resets its border flip-flops open. Firmware remains byte-identical; details below. |
| `979195d` | vic: benchmark frames with the screen and sprites on | **skip** | Benchmark prerequisite preceding PR #50. The full suite depends on absent sprite state and rendering. Blank/display benchmarks could be adapted separately, but are not a prerequisite or evidence of benefit for this branch's already-specialized reload path. |
| `2af7107` | vic: decide the sequencer reload once per cycle | **skip** | PR #50 removes eight calls to `advanceGraphicsData` in main's XSCROLL/multicolor renderer. This branch has neither that helper nor `loadGraphicsData`: only `dotclock7` commits pending data, guarded by one slot-range check. No redundant per-dot reload decision exists to hoist. |
| `cae6672` | test: retain exhaustive VIC reload timing oracle | **skip** | Exhaustively verifies the new `reloadDot` against main's old XSCROLL/multicolor phase rule. Neither the helper nor those graphics-mode semantics exist here; `vic_modes_test.go` is absent. |
| `4a37e5b` | vic: decode the graphics colours ahead of the dots that use them | **skip** | PR #51 caches main's graphics-mode colour decode and foreground classification at reload/background writes. TinyGo has no mode dispatch, multicolor sequencer or sprite foreground classification: each dot directly selects background0 or the latched standard-text foreground. The targeted repeated decode is absent. |

## PR #47 revalidation on current TinyGo: 2026-09-15

Reconsidered bank-out head `49278dabe595757d1964c312806b602ab58432de`
against fetched `origin/tinygo` at `210267b`, after the two-colour lookup
was merged. This evaluates adoption into TinyGo, not merge readiness
against main. The local branch at `4be5ba5` has identical source to that
remote baseline, with additional review documentation.

Applied the previously reviewed seven-file adaptation in a detached
worktree based on `210267b`. It retains the lightweight virtual drive and
cached standard-text renderer. Host `go test -count=1 ./...`,
`go build ./...`, and `go vet ./...` passed. Repeated focused tests cover
the wedge, latch, colour cache, and additional checks for loading and
executing machine code at `$8000`, reinstalling the released wedge on
machine reset, and I/O1 decoding across all CPU banking-bit combinations.
Both device builds passed with TinyGo 0.42.0 / LLVM 22.1.4.

Fresh Tufty MAZE A/B/A, `-target=tufty2040 -opt=2 -scheduler=none`,
ten 50-frame windows ending at frames 500 through 950:

| run | mean ms/frame | minimum window mean | maximum window mean |
|---|---:|---:|---:|
| current TinyGo A before | 74.232464 | 74.214680 | 74.249900 |
| bank-out B | 74.460164 | 74.437680 | 74.487620 |
| restored current TinyGo A after | 74.232108 | 74.213380 | 74.250600 |

Against the averaged baseline of 74.232286ms/frame, bank-out regresses by
**0.227878ms/frame (0.3070%)**. Baseline means differ by only 0.000356ms,
and candidate window means do not overlap either baseline range.
This is less than the previous 0.7447% regression, but still fails the
strict no-regression gate. The memory benefit remains 8192 additional
BASIC bytes (38911 total), with reactivation changed to `SYS 828`.

Tufty ELF text grows from 200632 to 203404 bytes, data stays 328 bytes,
and BSS grows from 223336 to 223344 bytes. Baseline loadable image matches
the accepted two-colour candidate exactly:
`a7225146a3ad3918ae86b5c9bd36ecd959ca83bd38587ca8e3d12d58eb599ca2`.
Bank-out image:
`ff4539fa9ac676335487d8fc56b03b5bb717845fc2f35ba40c828cf8766064cf`.
The Badge build (`-opt=2 -scheduler=cores`) remains byte-identical to its
accepted baseline; no Badge hardware timing was performed. No specific
code/layout cause is claimed for the Tufty regression.

**Defer adoption.** No source backport or PR merge was performed. The
current lookup-enabled baseline firmware was restored and verified by
the final A capture. Candidate patch/tests, ELF images and raw captures
are preserved as `bankout-lookup-*` session artifacts. Both the current
TinyGo trial and the cancelled, incorrectly targeted main-review worktree
were removed after preserving their artifacts. No main-review result was
used to decide the TinyGo gate.

## Two-colour lookup experiment: 2026-09-15

After the PR #51 review, the maintainer requested a separate standard-text
lookup experiment. Tested in an isolated worktree based on `3a8df3e`,
without importing main's graphics modes, reload helper or sprite logic.

The candidate adds `gdColor [2]uint8` beside the sequencer and replaces
each dotclock's background/foreground branch with
`gdColor[gdSequencer >> 7]`. Reset initializes both entries, `$D021`
writes (including register mirrors) update entry zero, and the existing
`dotclock7` cell-boundary latch updates entry one from the foreground byte.
Sequencer shifts, border overrides and final four-bit colour masking remain
unchanged.

Host tests/build/vet and both device builds passed with Go 1.27.1 and
TinyGo 0.42.0 / LLVM 22.1.4. Added experimental tests exercise all 256
background values, all 256 foreground values, both high-bit choices and
all eight dotclock implementations against the original colour rule.
They also check reset initialization, a real CPU `$D021` store's pixel
timing, and cache consistency after every cycle of a frame containing
mirrored background writes and character reloads. Removing the background
refresh fails consistency at cycle 11; removing the foreground refresh
fails the colour oracle. Both mutations were restored before the accepted
candidate capture, and a rebuilt loadable image matched the original
candidate byte-for-byte.

Tufty A/B/A used the existing MAZE demo, `-opt=2 -scheduler=none`, and
ten 50-frame windows ending at frames 500 through 950:

| run | mean ms/frame | minimum window mean | maximum window mean |
|---|---:|---:|---:|
| baseline A before | 74.797638 | 74.768560 | 74.828940 |
| two-colour lookup B | 74.232228 | 74.215600 | 74.251500 |
| restored baseline A after | 74.797206 | 74.770060 | 74.829220 |

The baseline mean is 74.797422ms/frame. The candidate saves
**0.565194ms/frame (0.7556%)**, with no overlap between candidate and
baseline window ranges. Baseline means differ by only 0.000432ms.
This candidate passes the Tufty no-regression gate; no Badge hardware
timing was measured.

Tufty ELF text shrinks from 200672 to 200632 bytes; data stays 328 bytes
and BSS grows from 223328 to 223336 bytes. The actual `dotclock6` colour
selection becomes a shift, address addition and byte load instead of the
old conditional background/foreground loads; its symbol shrinks from
220 to 212 bytes. `main.main` shrinks by 32 bytes and moves eight bytes.
The CPU load/TickPhi2 symbol addresses and sizes are unchanged. These are
consistent with less pixel-path work, but changes to flash/RAM placement
mean the measured gain is not attributed exclusively to removed branches.
Saturated XIP windows were not used for cache conclusions.

The rebuilt baseline's loadable SHA-256 remains
`50dd1fa34a378749742d85fb865dbe0f59682b5814a6e4493bf55b57564fc632`;
the candidate is
`a7225146a3ad3918ae86b5c9bd36ecd959ca83bd38587ca8e3d12d58eb599ca2`.
The accepted baseline firmware was restored and verified by the final
serial run. No source change was adopted: this was an experiment.
The complete source/test patch, ELF/disassembly and raw timing logs are
preserved in session artifacts under `color-lookup-*`; the isolated
worktree was removed. The patch is `color-lookup-experiment.patch`,
SHA-256 `fe8364c86478c65c6b0404a50101d6cd99b9c2cf5e6173bdec82306deeb58439`.

## PR #51 palette-cache review: 2026-09-15

At 03:40:01 UTC, GitHub REST confirmed PR #51 merged at 03:39:46 UTC,
as `4a37e5b82d66d594cab283f9a8758472fcfaa966`. Fetch and
`git merge-base --is-ancestor` verified the commit on `origin/main`.
Reviewed the complete implementation and all three new tests against
TinyGo at `a77dcba`. The table now covers all 44 mainline commits through
this checkpoint.

Main replaces `nextGraphicsColor`'s per-dot graphics-mode dispatch with
a four-entry `gdColor` cache and a `gdForeground` bit mask. It refreshes
them on reset, graphics-data reload, and writes to `$D021-$D024`. The
exhaustive test covers the old mode decode, the frame test checks cache
invalidation, and the background-write test checks next-cycle visibility.

This branch has no `nextGraphicsColor`, `loadGraphicsData`, graphics-mode
dispatch, multicolor shift state, or sprite collision/priority consumer.
Its dotclocks already perform only the standard-text choice:
`background0` when the sequencer's high bit is clear, otherwise
`byte(videoBuffer >> 8)`, followed by a shift and border override.
`$D021` writes update the value read directly by subsequent pixels.
`dotclock7` latches `videoBufferPending` once at each active cell boundary.
There is no repeated mode decode to lift out of this path.

The patch fails `git apply --check` in `6569.go` and requires absent
`vic_modes_test.go`. Importing the cache wholesale would require graphics
features deliberately excluded from this branch. A reduced two-colour
lookup could be investigated independently, but would introduce cache
maintenance and change generated code/layout; this review neither measures
nor claims a benefit or regression for that different optimization.

The existing alignment, CPU-to-VIC pixel ordering, and border tests passed:

```sh
go test -count=1 -run 'TestVIC(GAccessPixelAlignment|StepCycleCPUWriteFollowsPixels|BorderPlacement)' .
```

No production or test source was changed, and no firmware was built or
flashed for this semantic exclusion. The PR's performance figures do not
establish a TinyGo speedup. Its separately noted VC-wrap bug is not fixed
by this PR and was not included in this backport assessment.

## PR #50 reload-hoist review: 2026-09-15

At 03:20:36 UTC, GitHub REST confirmed PR #50 merged at 03:20:13 UTC,
with final commit `cae667202833b35baa44f04857c04a76b5cf32a5`. Fetch and
`git merge-base --is-ancestor` verified that commit on `origin/main`.
Reviewed both PR commits plus preceding benchmark commit `979195d`;
the table covers all 43 mainline commits through this checkpoint.
The TinyGo baseline was `2ed36d5`.

Main's reload phase depends on XSCROLL and multicolor mode. Previously,
each dot called `advanceGraphicsData` to decide whether to load; PR #50
computes the target dot once and passes it to all eight dotclocks.
The performance figures in the PR concern that different implementation,
not a measured improvement on this branch.

TinyGo's standard-text path already specializes the fixed phase:
`cycleGAccess` latches pending graphics data during Phi1, and `dotclock7`
alone commits it at the next phase-zero boundary, four dots later.
Its single `slot >= 6 && slot <= 45` check needs no dynamic phase
calculation. Dotclocks 0 through 6 have no reload checks or helper calls
to remove. Introducing `reloadDot` and comparisons in every dotclock
would add machinery rather than perform the proposed optimization.
Restoring the skipped graphics-mode implementation just to enable this
hoist would violate the scope of the backport.

The combined PR patch fails `git apply --check` in `6569.go` and
`vic_test.go`, and requires absent `vic_modes_test.go`. This is a semantic
exclusion, not just a patch-conflict decision. The retained tests passed:

```sh
go test -count=1 -run 'TestVIC(GAccessPixelAlignment|StepCycleCPUWriteFollowsPixels|StepCycleStalledReadClocksCIAs)' .
```

No source changes, candidate firmware, or hardware timing run were needed.
The upstream exhaustive oracle tests an absent helper; it is not a
standalone test backport. Record PR #50 as skipped, not as a measured
regression or a new TinyGo speedup.

## Review and Tufty A/B/A: 2026-09-15

Reviewed the 13 commits after the previous checkpoint, `a0c5bbe`, through
`origin/main` at `90081d26b3035f92d13741b70e37c99dde064fd7`, confirmed by
`git fetch origin` at 00:09:40 UTC. The earlier 17 logged decisions remain
unchanged; that checkpoint accounted for all 30 mainline commits since
`78e8f0e`. The baseline was `tinygo` at
`b6dab7f17eab7bb7f03caecbd4c568ed9779bf19`.

The cartridge trial was isolated from `tinygo`: apply `c0ef07e`, `be1a4e1`,
`7093968`, `0882c98`, and `90081d2`, in that order. Besides the PLA test
conflict described above, resolve `7093968` by removing `resetDOSWedge`
and the obsolete wedge-state snapshot without restoring `ResetDrive`,
the real-drive fields, or `driveResetDisk`. Retain the virtual-drive
snapshot and restore. No VIC accuracy changes were included.

Both baseline and cartridge trial passed `go test ./...`, `go build ./...`,
and `go vet ./...` with Go 1.27.1. The trial additionally passed
`go test -count=3 -run 'Test(DOSWedge|PLALoad)' .`. TinyGo 0.42.0 / LLVM
22.1.4 built both variants using:

```sh
tinygo build -target=tufty2040 -opt=2 -scheduler=none -o tufty.uf2 ./cmd/tufty2040
tinygo build -target=gopher-badge -opt=2 -scheduler=cores -o badge.uf2 ./cmd/gopher-badge64
```

The connected Tufty initially appeared as the `RPI-RP2` bootloader volume.
After flashing it enumerated as `/dev/cu.usbmodem112201`. Each run used
`tinygo flash -target=tufty2040 -opt=2 -scheduler=none ./cmd/tufty2040`
(with `-port=/dev/cu.usbmodem112201` once serial was available). Measurements
used the existing MAZE demo and `emulate=` telemetry at 115200 baud, without
firmware instrumentation changes. Each result below is the mean of ten
50-frame windows ending at frames 500, 550, ..., 950, excluding boot and
program-loading windows.

| run | mean ms/frame | minimum window mean | maximum window mean |
|---|---:|---:|---:|
| baseline A | 74.707578 | 74.677880 | 74.736700 |
| cartridge B | 74.797776 | 74.769880 | 74.832160 |
| restored baseline A | 74.708014 | 74.677260 | 74.740320 |

The two baseline means differ by only 0.000436ms. Relative to their mean
(74.707796ms), the cartridge trial is 0.089980ms/frame slower (0.1204%);
its window range does not overlap either baseline's. This is a small
regression, not a large performance problem, but it does not pass this
branch's strict no-regression rule. It does not establish which part of
the series causes the difference, and no Gopher Badge timing was measured.
The baseline firmware was restored and its serial timing re-measured as
the final A run. At this initial review checkpoint no source backports were
retained; the subsequent explicit acceptance below supersedes that decision.

The XIP telemetry becomes invalid near frame 650 (over 100%, followed by
0/0 access deltas) in all three runs. Those samples were not used to infer
cache behavior or explain the timing difference; `emulate=` is measured
separately. This review did not change the telemetry implementation.

### Explicit acceptance of the cartridge series

On 2026-09-15 the maintainer requested the complete cartridge series be
backported after reviewing the measured regression. This is an explicit
exception to the no-regression gate for these five commits, not a change
to the branch's general backport rule.

The intervening investigation also measured the assembler-only intermediate
at 88.124586ms/frame. Its `CPU.load`, `CPU.TickPhi2`, and `main.main`
instruction sequences match baseline after normalizing relocated addresses,
but the smaller assembler shifts `main.main` and the embedded ROMs in flash.
Across unsaturated windows ending at frames 200 through 600, its XIP hit
rate was 99.1831%, versus baseline's 99.9605%. This is evidence of sensitivity
to flash placement, not evidence that the complete series loses 13ms.
A linker-only padding experiment was built and verified to restore the
baseline hot-function/ROM addresses, but was not measured on hardware before
the decision to backport. The decoder-reordering experiment was not adopted.
These experiments do not conclusively attribute the full series' 0.09ms cost.

The accepted checkout passed `go test ./...`, `go build ./...`, and
`go vet ./...`, plus both device builds with the target-specific scheduler
flags above. Its loadable Tufty image is byte-identical to the tested full
cartridge trial (`arm-none-eabi-objcopy -O binary`, then `cmp`). The accepted
firmware was flashed to the Tufty and left running: a fresh ten-window
capture ending at frames 500 through 950 measured 74.797738ms/frame
(window range 74.769480-74.830820ms), reproducing the earlier cartridge run.

## Upstream branch review: 2026-09-15

Fetched at 00:28:01 UTC with `origin/main` at `672c3f8`. This checkpoint
covered all 37 commits from `78e8f0e` through that pinned mainline tip.
Trials below used the accepted cartridge baseline `b65f8ab`; none of these
new source candidates was adopted during this review.

The remaining remote branches at that snapshot were:

| branch (under `origin/`) | reviewed tip | disposition |
|---|---|---|
| `worktree-stepcycle-only` | `5be22fc` | Eligible together with its follow-up; final upstream integration pending. |
| `davecheney-stepdot-removal-review` | `74fcf1c` | Follow-up to StepCycle; see combined trial below. |
| `wedge-bank-out` | `49278da` | Functional, but measured Tufty regression; defer. |
| `copilot/full-crt-support` | `17fc62c` | Included in successor CRT branch; do not take independently. |
| `davecheney-crt-support-testing` | `789d0d0` | Static-CRT adaptation needs corrections and Tufty timing; defer. |
| `paletted-framebuffer` | `8104e24` | Seven commits patch-equivalent to the new mainline framebuffer series; desktop-only. |
| `dfc/tinygo-backports` | `5cf19bc` | Three remaining differing hashes are patch-equivalent to changes already on this branch. |
| `copilot/improve-dos-wedge-implementation` | `a6b9546` | Superseded RAM-wedge alternative: cached installation through a PLA store hook and `-kernal stock\|wedge`, rather than the accepted cartridge. |
| `davecheney-dfc/vic-mask-uint8-hot-path` | `d5d4a64` | Historical mask optimization family, not reopened or freshly hardware-tested. |
| `dfc/backup-vic-hoist-20260911` | `c61d0bd` | Historical hoist alternative, not reopened or freshly hardware-tested. |
| `dfc/backup-vic-hoist-f06b4c9` | `f06b4c9` | Historical hoist alternative, not reopened or freshly hardware-tested. |
| `dfc/vic-hoist-commits-2-4` | `8b6a30f` | Previously rejected hoist series documented below; not remeasured. |

The mask/hoist alternatives are not all patch-identical. Their disposition
does not claim a new timing result for each branch.

### StepCycle-only candidate

Reviewed `5be22fc` with `74fcf1c`, adapting only the existing TinyGo
surfaces. The combined candidate removes public `StepDot`, package
`FinishFrame`, and `VICII.FinishFrame`, migrates callers/tests, and corrects
CLI trace timing. It preserves all 25 retained VIC function bodies,
including `stepCycle`, and does not import absent graphics, sprite or IRQ
features. The follow-up fixes an upstream sprite test that resumed cycle
stepping at dot 51, making its dot-48 target unreachable; that test is absent
here.

Host tests/build/vet and both device builds passed. A CPU-before-IEC
ordering mutation was rejected by the new test. Extracted loadable images
were byte-identical to the accepted baseline on both targets:

| target | SHA-256 |
|---|---|
| Tufty | `50dd1fa34a378749742d85fb865dbe0f59682b5814a6e4493bf55b57564fc632` |
| Badge | `47f7fdf9ee60bb29782f2cf127061e9b78d2abdd7259152ced7decf3fa2517b1` |

No new hardware timing was needed for these identical device executables.
On 2026-09-15 the maintainer authorized backporting the finalized series
**after it merges upstream**, not applying this trial immediately. The
separate Step cycle review is adding regression tests and comment fixes;
the final merged revision must be reconciled with this candidate and
validated again. At 00:36:03 UTC GitHub reported PR #49 open, not merged.

### Final StepCycle backport after merge

At 00:48:10 UTC, GitHub REST reported PR #49 merged at 00:42:19 UTC,
with final merged commit `dceb72121d24223509b9913a9fd88093b63492b5`.
After fetching, `git merge-base --is-ancestor` verified that commit on
`origin/main`. The merged series is `aa710d2`, `98a2b37`, `dceb721`;
its final tree equals reviewed head `c7124de`. These are the final
mainline hashes, not the superseded pre-rebase review hashes.

Backported the complete applicable series onto `9211293`, preserving the
earlier review log. Coverage now reaches all 40 mainline commits through
`dceb721`. The first two commits use the tested adaptation above; the
final commit adds its new tests and corrected comments. No sprite, IRQ,
indexed framebuffer or real-drive implementation was introduced.

The new CPU-to-VIC pixel test initially failed because upstream resets
its border flip-flops closed, whereas this branch resets them open.
The test now explicitly sets `verticalBorder` to keep its pixel samples
in the border. Production reset/rendering behavior is unchanged. Moving
the CPU tick before the pixels made the adapted test fail at dot 41;
restoring the correct order made it pass.

`go test ./...`, `go build ./...`, `go vet ./...`, repeated timing tests
and repeated CLI trace tests passed. Both target-specific TinyGo builds
passed, and their extracted loadable image hashes exactly match the
accepted baseline hashes in the table above (Tufty also checked with
`cmp`). All retained VIC function bodies remain unchanged.
No new hardware measurement or flash was necessary for byte-identical
firmware. This satisfies the default no-regression gate without another
exception. The backport is local only; nothing was pushed.

### Cartridge bank-out candidate

`49278da` adds an I/O1 release latch at `$DE00-$DEFF`, cleared on reset,
and small screen-page/cassette-buffer stubs. It restores 38911 BASIC bytes
and RAM at `$8000`, with reactivation changed to `SYS 828`. This is a
separate change from the five explicitly accepted cartridge commits.

The isolated adaptation passed host tests/build/vet and both device builds.
Using the same MAZE workload, compiler flags, and ten windows ending at
frames 500 through 950:

| run | mean ms/frame |
|---|---:|
| accepted cartridge A before | 74.797738 |
| bank-out B | 75.354062 |
| restored accepted cartridge A after | 74.796380 |

Against the averaged baseline of 74.797059ms/frame, bank-out costs
0.557003ms/frame (0.7447%). It fails the default no-regression gate.
The accepted firmware was restored and remeasured for the final A run.
The earlier cartridge exception does not authorize this regression.

### CRT candidates

The successor includes all seven original CRT commits (`1dc9e8c`,
`dab9cfa`, `6dda3bd`, `63328ef`, `b52891c`, `c2782cc`, `17fc62c`) and
four follow-ups (`c4f507a`, `0af1482`, `76e975f`, `789d0d0`).
Do not merge its ancestry wholesale: it includes rejected mainline work.
The intermediate command deletion and reversal cancel each other;
`789d0d0` finally supplies the DiSTestMAX CRT fixture with that cleanup.

A static type-0, bank-zero candidate was adapted and tested, but is deferred.
Its practical loading benefit is desktop/headless, and its Tufty executable
changes without a hardware timing result. It requires these corrections
rather than a verbatim cherry-pick:

- Hardware type 1 is Action Replay, not Ultimax. Static Ultimax uses type 0
  and the GAME/EXROM header lines. Reject unsupported bank-switching types.
- Reject empty/duplicate CHIP windows, invalid line values and incompatible
  sizes/addresses. Added tests expose 13 failures in the unchanged upstream
  loader; corrected parsing passes them.
- Normal 16K ROMH depends on HIRAM independently of LORAM. Upstream's
  combined condition fails mapping tests at CPU-port values 2 and 6.
- Extend frontend cartridge exclusivity so CRT, wedge and diagnostics do
  not silently replace each other.
- Migrate all three TinyGo cartridge test literals atomically with the
  public `Cartridge.ROM` removal and new ROML/ROMH representation.

The candidate preserves accepted 8K wedge banking and RAM writes beneath
ROM, virtual-drive behavior, reset behavior and the lightweight renderer.
It does not import upstream CIA2 video-bank or sprite mapping logic. It
does not implement full Ultimax RAM holes/I/O/write decoding or arbitrary
bank-switching hardware.

Host tests/build/vet, repeated wedge/parser/mapping tests, 23 CLI checks,
and both device builds passed. Loading the wedge through the CRT parser
also passed a virtual-drive load test. The CRT diagnostic fixture's ROMH
payload equals `rom.DiagCart`; CRT and existing headless diagnostic startup
reach the same 100000-cycle checkpoint. Badge firmware is byte-identical;
Tufty is not (text +408 bytes, BSS +28 bytes). No CRT candidate was flashed.
At 00:35:34 UTC GitHub reported PR #39 closed without merging.

All review-owned detached worktrees were removed. Tested StepCycle,
bank-out and corrected CRT patches were preserved as session artifacts.
Bank-out and CRT remain unadopted; StepCycle was subsequently backported
after its verified merge as recorded above.

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

## Two-colour lookup adoption and validation: 2026-09-15

Adopt the independently tested TinyGo optimization on `2ed36d5`: each of
the eight dotclocks selects `gdColor[gdSequencer>>7]` instead of branching
between background and foreground. Reset initializes the pair from the
retained background register and zero foreground; `$D021` and its mirrors
update the background immediately, and the existing dotclock7 graphics
latch updates the foreground. This is not a backport of main PR #51 and
does not introduce graphics modes, sprites, or PR #50/#51 helpers.

Tufty 2040 MAZE A/B/A used TinyGo 0.42.0, LLVM 22.1.4 and Go 1.27.1,
with `-target=tufty2040 -opt=2 -scheduler=none`. Each run used ten
50-frame windows ending at frames 500 through 950:

| firmware | mean ms/frame | window range ms/frame |
|---|---:|---:|
| Baseline before | 74.797638 | 74.768560-74.828940 |
| Two-colour lookup | 74.232228 | 74.215600-74.251500 |
| Restored baseline | 74.797206 | 74.770060-74.829220 |

Against the average baseline of 74.797422ms/frame, the candidate saves
**0.565194ms/frame (0.7556%)**; baseline drift is 0.000432ms/frame.
Tufty text shrinks from 200672 to 200632 bytes, data stays at 328 bytes,
and BSS intentionally grows from 223328 to 223336 bytes (+8).
`dotclock6` shrinks from 220 to 212 bytes, with shift/add/byte-load colour
selection replacing conditional loads. `main.main` shrinks by 32 bytes
and moves by 8 bytes; layout changes prevent attributing the whole gain
exclusively to branch removal. `CPU.load` and `TickPhi2` retain their
sizes and addresses. Late-window XIP counters saturate, so they do not
support cache conclusions.

Validation covers all background/foreground byte values and both
sequencer high-bit choices across all eight dotclocks, reset, actual CPU
`$D021` pixel timing, and a full frame of mirrored writes/cache consistency.
The experiment's background-update mutation failed at cycle 11 and its
foreground-update mutation failed the pixel oracle; both were restored.
Final host checks are `go test -count=1 ./...`, `go build ./...`, and
`go vet ./...`. Both device firmware builds pass, using the Tufty flags
above and `-target=gopher-badge -opt=2 -scheduler=cores` for the Badge.
The extracted final images match the experiment images byte-for-byte;
the Tufty candidate SHA-256 is
`a7225146a3ad3918ae86b5c9bd36ecd959ca83bd38587ca8e3d12d58eb599ca2`.
No Badge hardware timing was performed. The experiment restored the
physical Tufty baseline; PR preparation did not flash hardware.

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

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

## Independent #57 trial: no demonstrated improvement, 2026-09-15

**Accepted explicitly after measurement:** at 22:19 AEST the user instructed
"merge #57 onto origin/tinygo, #61 can rebase". This supersedes the earlier
conditional decision below. The measured result remains effectively flat,
not a demonstrated speedup. Adopt only this standalone #57 adaptation on
the recovered lightweight baseline; #54 and #56 remain excluded.

The canonical `tinygo` branch is restored to this lightweight history.
Before replacement, preserve its main-based #61 head
`142f67892a29dda180d6f69a0202567d8de96fbc` on
`dfc/pr61-mainline-preserved`. Use an exact-SHA force-with-lease for `tinygo`
so a concurrent update cannot be overwritten. #61's mainline changes must
be migrated/rebased separately, not reapplied wholesale to this keeper.

The user requested #57 alone against the accepted baseline, with adoption
conditional on improvement. Source candidate `4fc9ccc` starts from recovery
tip `2c34100` and changes only RESTORE handling, its tests and documentation.
It removes the per-Phi2 keyboard pulse countdown and directly sets the
existing `nmiLatch`/`nmiLatchClock` on a RESTORE press. CIA2 remains sampled
at opcode fetch, and the existing IRQ delay/mask handling and AEC stall
behavior are unchanged. No #54 clocked IRQ/write-cycle table or #56 CIA
notification framework is included.

As in #57, RESTORE can deliver an NMI while CIA2 holds its line asserted,
and frontends must call it once per press edge. Tests cover that overlap,
fresh CIA2 edges, recognition delay, holds, repeat presses, reset, release,
unconnected keyboards and existing KERNAL/wedge behavior.

Host `go test -count=1 ./...`, `go build ./...`, `go vet ./...` and focused
RESTORE/reset/NMI/wedge checks pass. Both TinyGo target builds pass with
TinyGo 0.42.0 / LLVM 22.1.4 / Go 1.27.1, `-opt=2`, Tufty
`-scheduler=none`, Badge `-scheduler=cores`.

| Tufty firmware | Flash | Data | BSS | Static RAM including 4096 stack bytes |
|---|---:|---:|---:|---:|
| Baseline | 199580 | 328 | 223344 | 227768 |
| #57 only | 199724 | 328 | 223340 | 227764 |

Tufty flash grows 144 bytes despite the source simplification; static RAM
falls four bytes. Badge flash/RAM are 145680/226744 bytes, compared with
145920/226752 for baseline. Size does not predict frame-time improvement.

Reused the already-measured baseline without flashing it again. The one
new #57-only run used the same MAZE workload, ten 50-frame windows ending
at frames 500 through 950, and continued through frame 1000.

| Firmware | Mean ms/frame | Window range ms/frame |
|---|---:|---:|
| Baseline, reused A-restored | 74.855054 | 74.831740-74.888820 |
| #57 only | 74.868554 | 74.850760-74.894560 |

The observed difference is **+0.013500ms/frame (+0.018035%)**, not an
improvement. Windows overlap and this is a sequential comparison with a
reused baseline, not evidence for a precisely established tiny regression.
The conditional approval to merge on improvement is therefore **not met**.
Keep this as an isolated trial; no adoption, PR merge or push was performed.

The #57-only image was left on the Tufty, as requested for these comparisons;
no baseline reflash followed. Its copy completed at 12:17:12 UTC and serial
reached frame 1000 at 12:18:29 UTC without reported panic/fatal/out-of-memory.
Identity evidence is the verified UF2 payload, successful bootloader copy
and serial progress, not flash readback or visual/interactive verification.
Loadable SHA-256:
`17cb0b3bdff29c0939ca5cd2704f59d416396809747980dd81f89ed37da3a938`.
UF2 SHA-256:
`43a8be14010a666399fed780117e540623a7d3b05f515340448e6098428c8b53`.

Source and hardware artifacts are in session
`1c4c73da-12e2-4bc3-98bb-878099dafd16/files/restore-only/`, including
`comparison.json`, hardware logs/provenance, and both board ELFs. The script
is `files/hardware_restore_only.py`. The earlier combined trial and its
reports remain preserved under `refs/backport-recovery/1c4c73da-full-irq-trial`
at `e00f58b`; they were not discarded when this independent candidate began.

## PR #63 CIA interrupt-check hoist: 2026-09-15

**Accepted:** at 22:26 AEST the user explicitly accepted #63 as a backport
to `origin/tinygo`, after reviewing the 2.428753% Tufty improvement.
The implementation `e207856` and measurement record `cb4dae0` are published
on that branch. This acceptance is for the lightweight adaptation, not a
merge of the upstream PR into main.

At the user's explicit request, adapt #63's head `a710211` onto the accepted
#57-only TinyGo baseline `0a95e47`; upstream #63 was open at inspection.
Local source commit is `e207856`. Keep the lightweight CIA's direct
`IRQ = true` output rather than importing #56's notifications. Move the
ICR/IMR eligibility check from every Tick to Timer A/B underflows and mask
writes. No #54/#56, renderer or real-drive changes are included.

Mask writes now assert an eligible line immediately, rather than waiting
for Tick. New IRQ and NMI tests ran against both the unchanged baseline
and candidate: `STA $DC0D/$DD0D` unmasking a stopped timer's latched flag
still enters at cycle 7, PC `$0204`. Thus the upstream timing argument
was checked against TinyGo's older opcode-fetch sampling rather than
assuming main's clocked IRQ machinery exists. Additional tests cover both
timer sources, masked underflows, immediate unmasking, mask clearing
without acknowledgement, and acknowledgement without spurious reassertion.

All host tests, build and vet pass, as do both device builds. Binary
analysis was reported to the user before flashing. TinyGo 0.42.0,
LLVM 22.1.4, Go 1.27.1, `-opt=2`; Tufty `-scheduler=none`, Badge
`-scheduler=cores`.

| Metric | #57 baseline | #57+#63 | Delta |
|---|---:|---:|---:|
| Tufty flash | 199724 | 199476 | -248 |
| Tufty data | 328 | 328 | 0 |
| Tufty BSS | 223340 | 223340 | 0 |
| Tufty static RAM, including stacks | 227764 | 227764 | 0 |
| Badge flash | 145680 | 145432 | -248 |
| Badge static RAM, including stacks | 226744 | 226744 | 0 |
| CPU.TickPhi2 symbol | 11196 | 10932 | -264 |
| CIA.Store symbol | 226 | 242 | +16 |

Both CIAs remain inlined into CPU.TickPhi2; checkIRQ is also inlined.
No new out-of-line helper calls appear. Non-underflow cycles skip both
ICR/IMR checks and their branches. Static conditional-branch instruction
counts across CPU.TickPhi2 change from 133 to 131, and CIA.Store from 19
to 20. These are static counts, not executed branch counts. CIA.Load and
main.main sizes are unchanged; CPU.TickPhi2's stack-frame prologue remains
unchanged, while register allocation and internal layout change. The
248-byte flash saving is accounted for by TickPhi2 -264 and Store +16.

Reused the previous #57-only baseline, without another baseline flash.
One candidate run used the same MAZE workload and ten 50-frame windows
ending at frames 500 through 950, continuing through frame 1000:

| Firmware | Mean ms/frame | Window range ms/frame |
|---|---:|---:|
| #57 only, reused | 74.868554 | 74.850760-74.894560 |
| #57+#63 | 73.050182 | 73.013320-73.075780 |

The measured saving is **1.818372ms/frame (2.428753%)**, with no overlap
between window ranges. This is a sequential comparison against a reused
baseline, not a new A/B/A. It supports taking this adaptation under the
requested comparison, without claiming exclusive attribution to branch
removal rather than generated layout. XIP counters saturate, so no
cache-hit attribution is made.

The candidate was left running. Verified UF2 copy completed at 12:24:37 UTC;
serial reached frame 1000 at 12:25:52 UTC without reported panic/fatal/OOM.
Boot/progress is not pixel verification, actual heap measurement or physical
RESTORE input coverage. Identity evidence is the checked UF2 payload,
successful bootloader copy and serial progress, not flash readback.

Loadable SHA-256:
`633f5fb477fe029cc267b866e1bab50afbb2a175fc082a53137d059488775480`.
UF2 SHA-256:
`49ee9953de514efb5d054a5ae37ca0e38fc2bf16ed60c8f1582d0316f01b7b4d`.
Artifacts in session `1c4c73da-12e2-4bc3-98bb-878099dafd16/files/cia-hoist/`
include both ELFs, UF2/BIN, section/symbol/disassembly reports, codegen JSON,
serial logs/provenance and `comparison.json`. Reproduction uses
`files/hardware_cia_hoist.py`. No native performance benchmark was run.

## PR #67 clock narrowing intermediate: 2026-09-15

**Accepted explicitly:** at 22:43 AEST the user instructed merging #67
into `origin/tinygo` and making it the baseline, accepting the documented
full-counter-period limitation below. GitHub rebase-merged #67 at
12:43:26 UTC as `9d6533090de4b56c9759943ed4846761b4fb8780`.
Its tree was verified identical to measured trial tip `5cf46d9`.
The new timing baseline is **71.544682 ms/frame**. No reflash was needed:
the last flashed image was already the measured #67 candidate.
This acceptance supersedes the isolated-trial/non-promotion verdict below;
the limitation and measurements remain unchanged.

The user requested resurrecting #67 before pursuing #71's clock removal.
Adapt upstream head `02e976c` onto accepted #57+#63 tip `6f774d8`.
Source candidate: `3588d64ce6fd2c5a5ce8495d8c348eae4b606584`.
Upstream #67 was closed at 12:33:13 UTC; it was not reopened.
This is an isolated experimental intermediate, not yet an accepted keeper
update. At that observation, `git ls-remote origin refs/heads/tinygo`
still returned `6f774d83ac6c074580436f468f202d7b29a0f858`.
#71 remains deferred; none of its production changes are included.

Unlike main, this branch still uses the clock for IRQ recognition.
Narrow all three fields (`Clock`, `irqAssertClock`, `nmiLatchClock`) to
`uint`, and use unsigned elapsed differences for both interrupt delays.
Preserve opcode-fetch-only line sampling, reset history, effective-I
handling, RESTORE and the lightweight renderer/virtual drive. Replace the
Tufty demo's absolute deadline with a start/duration pair, retaining its
40000-cycle key phases. `CPU.Clock` changes its exported type.

Host `go test -count=1 ./...`, `go build ./...`, `go vet ./...` and both
device builds pass. New tests compare 30000 randomized cycles against the
old absolute-clock oracle, including IRQ/NMI edges, RESTORE, masks, reset
and AEC holds. Separate traces cover rollover for IRQ, CIA2 and RESTORE.
The hardware-tagged demo can be tested without the frontend using
`go test cmd/tufty2040/demo.go cmd/tufty2040/demo_test.go`; this verifies
key press/release timing across rollover.

**Long-duration limitation:** elapsed subtraction is unambiguous only
before a whole counter period has elapsed. A 32-bit counter wraps after
roughly 71 minutes of emulated time. In particular, an IRQ line retained
for a whole period can briefly appear younger than two cycles again.
The tests cover crossing rollover with short delays, not that full-period
case. Do not treat this intermediate as universally wrap-safe or silently
promote it to the keeper on performance evidence alone. A mature-delay
state/countdown would avoid this ambiguity; that needs separate evaluation
in the old TinyGo IRQ model before acceptance.

The following section/symbol analysis was reported before flashing, using
TinyGo 0.42.0 / LLVM 22.1.4 / Go 1.27.1, `-opt=2`, Tufty
`-scheduler=none`, Badge `-scheduler=cores`:

| Bytes | #57+#63 | +#67 | Delta |
|---|---:|---:|---:|
| Tufty flash | 199476 | 198588 | -888 |
| Tufty data | 328 | 328 | 0 |
| Tufty BSS | 223340 | 223308 | -32 |
| Tufty static RAM including stacks | 227764 | 227732 | -32 |
| Badge flash | 145432 | 144656 | -776 |
| Badge data | 260 | 260 | 0 |
| Badge BSS | 222388 | 222360 | -28 |
| Badge static RAM including stacks | 226744 | 226716 | -28 |
| CPU.TickPhi2, both targets | 10932 | 10164 | -768 |
| Tufty main.main | 11496 | 11388 | -108 |
| Badge main.main | 5036 | 5036 | 0 |

Reserved stacks remain 4096 bytes on both boards. CIA.Load/Store remain
164/242 bytes, CIA.Tick/checkIRQ remain inlined, and the demo wait helpers
are inlined. TickPhi2's static conditional-branch count falls 131 to 113.
Its prologue still saves r4-r7/lr and reserves 60 stack bytes. The clock
increment loses the high-word load/carry/store, zero setup and initial
spill. Static instruction/layout changes do not establish runtime savings.

One candidate flash reused the recorded #57+#63 baseline, with no baseline
reflash before or afterward. Same MAZE workload, ten 50-frame windows
ending at frames 500 through 950, continuing through frame 1000:

| Firmware | Mean ms/frame | Window range ms/frame |
|---|---:|---:|
| #57+#63, reused | 73.050182 | 73.013320-73.075780 |
| #57+#63+#67 | 71.544682 | 71.496220-71.585460 |

The candidate saves **1.505500 ms/frame (2.060912%)**, with non-overlapping
window ranges. This supports the performance side of the intermediate,
but does not resolve its long-duration counter limitation. No desktop
performance benchmark or cache attribution was used.

The candidate was left flashed. Copy completed at 12:31:43 UTC; serial
reached frame 1000 at 12:32:56 UTC without reported panic/fatal/OOM.
UF2 block structure/address continuity and payload equality to the
ELF-derived binary were checked before copying. This is not flash readback,
pixel/physical-input verification or actual heap measurement.

Loadable SHA-256:
`ef9803c2f81268d6acc02b3bde2e6ede663cce00440b93021f2c1bccf8e991e5`.
UF2 SHA-256:
`1275e34e212c4235d31347fcded14a0d04ecc4934587bcb4377129ba6c7bc7c5`.
Artifacts are in session
`1c4c73da-12e2-4bc3-98bb-878099dafd16/files/clock-uint/`: both ELFs,
BIN/UF2, source archive/patch, sections/symbols/disassembly, `codegen.json`,
hardware logs/provenance and `comparison.json`. Reproduction script:
`files/hardware_clock_uint.py`.

## Simplified interrupt recognition: 2026-09-15

**Accepted explicitly:** at 22:48 AEST the user requested committing this
implementation as the baseline on `origin/tinygo`. Adopt source `108e8ae`
and measurement record `e881196`, superseding #67 as the current baseline.
The new recorded baseline is **70.850726 ms/frame**. No additional flash
is needed; the measured no-delay candidate was already left installed.

At the user's request, remove synchronization timing rather than replacing
the clock with countdowns. TinyGo targets character-mode workloads, not
complex VIC effects or cycle-accurate interrupt timing; `main` remains the
accuracy-oriented branch. Source candidate `108e8ae159109698c9d51829ea3dd8aa15fb9943`
is based on accepted #67 tip `b0a96b0`.

IRQ checks CIA1's existing asserted level and the current status-register
I bit at the next unstalled opcode fetch. NMI retains priority, the existing
CIA2 edge latch and direct RESTORE events, but has no recognition delay.
Remove Clock, both timestamps, IRQ edge history and effectiveI entirely.
No interrupt bitmask/notification framework or countdown is introduced.
There is no interrupt-age arithmetic left to wrap, resolving #67's
full-counter-period ambiguity structurally.

This intentionally changes timing: CLI/SEI/PLP affect the next fetch
without the old I-flag pipeline delay; a CIA mask write is recognized on
cycle 5 at PC $0203 rather than cycle 7 at PC $0204 in the existing test.
Instruction execution, interrupt entry/stack/vector microcode, NMI
priority, IRQ level/acknowledgement, held-NMI suppression, and AEC stalls
are preserved. Short CIA2 pulses wholly between fetches can still be
missed, as with this branch's prior fetch-only sampling.

The exported CPU.Clock is removed. The Tufty demo uses two skipped frame
ticks between key transitions, preserving the old three-frame transition
cadence. IEC ordering tests use CIA2 timer A after KERNAL IOINIT rather
than retaining a production-only test clock. The two CIAs continue ticking
on every Phi2, including when the CPU is stalled.

Host tests, build and vet pass. New tests cover next-fetch recognition,
instruction completion, AEC holds, CLI/SEI/PLP/RTI mask behavior, and a
held masked IRQ followed by unmasking and acknowledgement. Existing NMI
priority/edge/reassertion, RESTORE overlap/reset, KERNAL and wedge tests
pass. Removed old delay-oracle/wrap tests because those requirements are
deliberately no longer implemented. The demo's old absolute-deadline
model remains a test oracle for its frame cadence, including shifted keys:
`go test cmd/tufty2040/demo.go cmd/tufty2040/demo_test.go`.

Both device builds pass with TinyGo 0.42.0 / LLVM 22.1.4 / Go 1.27.1,
`-opt=2`, Tufty `-scheduler=none`, Badge `-scheduler=cores`. The following
analysis was reported before flashing:

| Bytes | #67 baseline | Simple interrupts |
|---|---:|---:|
| Tufty flash | 198588 | 198324 |
| Tufty data | 328 | 328 |
| Tufty BSS | 223308 | 223292 |
| Tufty static RAM including stacks | 227732 | 227716 |
| Badge flash | 144656 | 144592 |
| Badge data | 260 | 260 |
| Badge BSS | 222360 | 222344 |
| Badge static RAM including stacks | 226716 | 226700 |
| CPU.TickPhi2, both targets | 10164 | 10120 |
| Tufty main.main | 11388 | 11208 |
| Badge main.main | 5036 | 5028 |

Reserved stacks remain 4096 bytes per target. CIA.Load/Store stay at
164/242 bytes; CIA.Tick/checkIRQ remain inlined. TickPhi2 static conditional
branches fall 113 to 96. The local frame shrinks 60 to 56 bytes and the
prologue saves r4-r6/lr rather than r4-r7/lr. The per-cycle counter increment
is gone, with no replacement interrupt timer work. Static code generation
does not by itself predict or attribute runtime savings.

One candidate flash reused #67's recorded MAZE baseline without reflashing
it. Ten 50-frame windows ending at frames 500 through 950:

| Firmware | Mean ms/frame | Window range ms/frame |
|---|---:|---:|
| #57+#63+#67, reused | 71.544682 | 71.496220-71.585460 |
| Simple interrupts | 70.850726 | 70.818040-70.892840 |

The measured saving is **0.693956 ms/frame (0.969962%)**, with
non-overlapping window ranges. This meets the performance gate for the
requested accuracy tradeoff; it is a sequential comparison, not a new
A/B/A, and not proof that every guest workload improves. No desktop
benchmark or attribution from the saturated XIP counters is used.

The candidate was left flashed. Copy completed at 12:46:10 UTC and serial
reached frame 1000 at 12:47:23 UTC without reported panic/fatal/OOM.
UF2 structure, block continuity and equality to the ELF-derived payload
were verified before copying. No flash readback, physical RESTORE/pixel
verification or actual heap measurement is claimed.

Loadable SHA-256:
`d8c06497675729ef3bf180a7a20534aabd746fd03fe915c3ca0398f7fe5e658c`.
UF2 SHA-256:
`547b7e28edab902e8e1bbbde9d171e16e448972510be84b97dde484e589b11bb`.
Artifacts: session `1c4c73da-12e2-4bc3-98bb-878099dafd16/files/simple-irq/`,
including both ELFs, source archive/patch, BIN/UF2, sections/symbols/
disassembly, `codegen.json`, hardware provenance/log and `comparison.json`.
One-shot measurement script: `files/hardware_simple_irq.py`.

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

## Accepted with performance exception: transparent DOS wedge (PR #55)

Backport the wedge from main PR #55 (`b679bfa`, merged as `4f8f7fb`) onto
`210267b`. The cartridge hides ROML during the real KERNAL memory test and
external calls, preserving all 38911 BASIC bytes. Its dispatcher, service
gate and workspace occupy `$033C-$03D1`; IO2 supplies the bootstrap aperture
and `$DFFF` mapping/kill latch. `@Q` now disables the cartridge until hardware
reset rather than supporting `SYS 32777` reactivation.

This is a wedge-only backport: the only CPU change is `cartridge.reset()`
before the existing reset. It does not include the separate IRQ/RDY/NMI
backport, new VIC behavior or the real 1541. The existing wildcard load/run
demo test is retained. Imported wedge tests explicitly attach a fresh virtual
drive, matching main's test helper; tinygo's `useDrive` otherwise preserves
an already-attached drive and its consumed startup status in nested tests.

Tufty MAZE A/B/A used TinyGo 0.42.0, LLVM 22.1.4 and Go 1.27.1 with
`-target=tufty2040 -opt=2 -scheduler=none`, taking ten 50-frame windows ending
at frames 500 through 950:

| firmware | mean ms/frame | window range ms/frame |
|---|---:|---:|
| Baseline before | 74.231756 | 74.214520-74.250140 |
| Transparent wedge | 74.855912 | 74.831660-74.890440 |
| Restored baseline | 74.232562 | 74.216520-74.251020 |

The new wedge costs **0.623753ms/frame (0.8403%)** against the averaged
baseline; baseline drift is 0.000806ms/frame. **The user explicitly accepted
this regression for the memory-compatibility improvement and requested the
backport. This is an exception, not a relaxation of the no-regression rule.**
The cost belongs to the complete wedge adaptation and generated layout;
no individual helper is identified as the cause. Late XIP counters saturate
and cannot support cache attribution.

Native Go on M4 Max, using twelve alternating one-second samples per
variant and `GOMAXPROCS=1`, showed no significant READY or MAZE frame-time
change (551.245 to 548.182us, p=0.128; 585.109 to 585.115us, p=0.630).
The isolated CPU-cycle benchmark increased from 7.891 to 8.219ns
(4.16%, p<0.001). These are emulator timings, not desktop presentation FPS.
Tufty flash grows from 196864 to 199580 bytes (+2716), RAM from 227760 to
227768 bytes (+8).

The full host suite passes, including full-memory LOAD/SAVE, mapping/kill
lifecycle, service register/flag preservation, IRQ handlers in high RAM,
and RESTORE/NMI gate boundaries on tinygo's existing CPU. The optional Uncle
Agnus fixture was not supplied and is not claimed as validated. All physical
captures reached frame 1000; baseline was restored through frame 1000 at
2026-09-15 06:47:46 UTC. Serial progress does not independently verify pixels
or broad game compatibility.

Final backport checks passed `go test -count=1 ./...`, `go build ./...`,
and `go vet ./...`. Both device builds pass: Tufty uses the flags above;
Gopher Badge uses `-target=gopher-badge -opt=2 -scheduler=cores`. The final
Tufty UF2 is byte-identical to the physically measured wedge image. Badge
flash/RAM are 145920/226752 bytes; no Badge hardware timing was performed.

## Queued after PR #55: IRQ, RESTORE and compact masks (#54, #56, #57, #58)

On 2026-09-15 the user requested PR #56, "Avoid idle IRQ work with
per-source interrupt notifications", be recorded for backport inclusion
on top of the transparent DOS wedge from PR #55. Apply and validate #55
first, then adapt #56 against that wedge-enabled TinyGo baseline.
GitHub REST at 07:49:49 UTC reported both PRs merged; the reviewed PR #56
head was `3f2fdcc4288ef6d8fdcb23c74b783f5c0fb0ab42`.

Status: **queued for backport, not implemented or validated here**.
The change tracks interrupt producers in a CPU-owned source mask and
avoids idle interrupt clocking while preserving pending requests. Its
upstream desktop benchmark improvements are not TinyGo hardware evidence.
Assess dependencies on main's IRQ/RDY/NMI semantics explicitly rather
than transplanting notification logic into this branch's different CPU
path without its prerequisites. Preserve the virtual drive and do not
restore unrelated VIC or real-1541 machinery.

The acceptance gate remains host tests/build/vet, both target-specific
device builds, and Tufty frame-time validation against the #55 baseline
at `-opt=2 -scheduler=none`. Badge uses `-opt=2 -scheduler=cores`.
The performance exception granted for #55 does not extend to #56.
This entry records the requested inclusion order; it does not claim a
completed backport or authorize an additional regression.

The user subsequently confirmed that #56 must include its #54 prerequisite
(clocked IRQ sampling, instruction-specific polls and RDY handling), then
added #57, "Latch RESTORE as a direct NMI trigger instead of a Phi2-clocked
pulse". The requested order is **existing #55, then #54 + #56 + #57 + #58**.
Offline preparation compares the #55 baseline, #54 alone, #54+#56, and
the full #54+#56+#57 candidate, retaining separate CPU-tick and device
size/RAM measurements. Hardware validation is deferred until available.

PR #57's reviewed head is `6d657ca242fba58df8d0d1d1c5706b2e1da7687f`.
It intentionally removes RESTORE pulse duration and allows a fresh press
to latch NMI even when CIA2 already holds the combined NMI line asserted.
That is a behavior change, not just branch removal: test and document
overlapping sources as well as ordinary RESTORE, RUN/STOP+RESTORE and
wedge service handoffs. Upstream's native benchmark results were broadly
flat; no TinyGo speedup or hardware acceptance is implied by inclusion.

The user added PR #58, "Shrink CPU write-cycle masks to uint8", to the
backport set. Its merged commit is
`db2fec6a83f4fbb7dd4890455a56d7e6a0ecfff7` (reviewed head `565aba8`).
Apply it after the IRQ/RESTORE adaptations: retain the mask literals,
use `[256]uint8`, and preserve the widened microcode-derived comparison
and eight-bit range guard in the tests. Upstream Tufty builds measured
256 fewer flash bytes with unchanged data/BSS; verify the actual saving
again on the combined TinyGo candidate rather than treating that as an
already-measured backport result. Inclusion is queued, not yet applied.
Only offline builds, size analysis and correctness tests are requested
until hardware is available; do not resume native performance benchmarks.

## Offline IRQ build checkpoint: 2026-09-15

Prepared isolated stages against `374465f` (the existing #55 backport).
No hardware was accessed and no IRQ candidate was adopted into this branch.
After the delegated preparation was asked to stop, the final #57 stage
was prepared directly in a separate worktree, reusing its committed
#54 (`0c6eeba`) and #56 (`57a6a5b`) adaptations. The final isolated
candidate is `6838252`. It keeps CIA1 IRQ/CIA2 NMI notifications without
adding the absent VIC raster IRQ source or real drive.

Tufty, TinyGo 0.42.0, `-opt=2 -scheduler=none`; all sizes are bytes:

| stage | flash (`-size=short`) | ELF `.data` | ELF `.bss` | reported RAM |
|---|---:|---:|---:|---:|
| A: #55 baseline | 199580 | 328 | 223344 | 227768 |
| B: +#54 | 200420 | 328 | 223328 | 227752 |
| C: +#54+#56 | 200836 | 328 | 223328 | 227752 |
| D: +#54+#56+#57 | 200820 | 328 | 223324 | 227748 |

The combined candidate adds 1240 flash bytes and reduces reported static
RAM by 20 bytes against A. Reported RAM includes two 2048-byte stack
reservations as well as `.data` and `.bss`; it is not measured runtime
heap use or proven free-memory headroom. GNU size's aggregate `text`
also counts those stack sections, so use the explicit TinyGo flash
figure rather than treating GNU `text + data` as flash consumption.

The directly prepared D stage passed `go test -count=1 ./...`,
`go build ./...` and `go vet ./...`, and both device builds. Badge
(`-opt=2 -scheduler=cores`) reports flash 146752 and RAM 226728 bytes.
D's Tufty loadable SHA-256 is
`96a79c77c1f5ca0a60ff5f6925c3f3d8773c5e36da570cf93f6b601b75438899`.
The combined patch is preserved as `irq-54-56-57-direct.patch` and
ELFs as `irq-D-tufty.elf` / `irq-D-badge.elf` in session artifacts.
The direct trial remains isolated for further validation.

This is a build checkpoint, not performance acceptance. Native benchmark
artifacts from the delegated run still require assessment; none of these
size figures establishes a CPU-tick speedup. Hardware A/B/A must compare
A against D using the same MAZE windows and target flags when the Tufty
becomes available.

## Table-free BA/RDY and AEC candidate

The isolated D candidate subsequently applied #58 as `70a09c4` (stage E).
Its Tufty flash was 200564 bytes, exactly 256 below D, with unchanged
`.data`, `.bss` and reported RAM. This remains isolated work, not adoption
of #54-#58 into this branch.

Stage F replaces the opcode write-cycle table with a predicate decoding
the implemented writing instruction families. It is evaluated only while
BA is low; the generated ARM code uses comparisons/arithmetic rather
than a replacement opcode-indexed table. Normal opcode/T-state dispatch
tables are unchanged in purpose.

BA now drives CPU read holds independently of AEC. The VIC counts three
completed BA-low cycles before withholding Phi2 ownership, including a
late badline, and immediately releases ownership when BA rises. CPU
external reads/writes are gated by AEC; CPU internal port accesses are not.
Synchronous reset-vector initialization bypasses the previous cycle's AEC.
The CLI counts actual held cycles rather than treating all AEC-low cycles
as read holds, and does not report the last bus access again while held.

The timing source is [Bauer section 2.4.3 and sections 3.14.3/3.14.6](https://www.cebix.net/VIC-Article.txt),
linked by [C64 Wiki's VIC-II page](https://www.c64-wiki.com/wiki/VIC-II).
There is no instruction-boundary BA handshake: writes can finish during
the warning, but the first read holds even in the middle of an instruction.

**Fidelity boundary:** a late c-access during the warning now reads `$FF`
for the character pointer rather than accessing RAM without ownership.
Its CPU-bus-derived colour is not implemented (the candidate uses zero).
Repeated held-read bus side effects remain unmodeled. An externally forced
AEC-low CPU read sees the last modeled bus byte, not an electrical bus
simulation. This is not a claim of complete FLI/VSP or transistor-level
bus fidelity; extending that model is separate from this focused change.

Tufty, TinyGo 0.42.0, `-opt=2 -scheduler=none`, bytes:

| stage | flash | ELF `.data` | ELF `.bss` | reported RAM |
|---|---:|---:|---:|---:|
| A: accepted #55 baseline | 199580 | 328 | 223344 | 227768 |
| E: +#54+#56+#57+#58 | 200564 | 328 | 223324 | 227748 |
| F: table-free BA/AEC separation | 200356 | 328 | 223324 | 227748 |

F is 208 flash bytes smaller than E, but still 776 bytes larger than A.
E-to-F symbol deltas are: table -256, `main.main` +48, `CPU.TickPhi2` +12,
`CPU.store` -8, `Reset` +4, `CPU.load` -4. These sum to -204; section
alignment accounts for the remaining -4. The VIC symbol grows by two
bytes (156 to 158), absorbed by existing alignment: total BSS and reserved
stacks are unchanged. Badge F builds with `-scheduler=cores`: flash 146352,
`.data` 260, `.bss` 222372, reported RAM 226728.

`go test ./...`, `go build ./...`, `go vet ./...` and both firmware builds
pass. The independent bus oracle covers all 256 opcode values and
read/write phases, including implemented illegal instructions; new tests
exercise held microcode state, BRK/IRQ/NMI write sequences, three warning
cycles, 40 stolen cycles, late takeover, release/reset and AEC-gated I/O.
The 24 interrupt reference schedules retain their expectations, with their
hold inputs moved from AEC to BA. Existing wedge, boot, keyboard and pixel
ordering tests pass.

Source changes remain in the isolated `irq-direct-trial` worktree,
committed as `fa530554589adffff28d16369083aa28f9ad8a86`.
Session artifacts include `irq-F-{tufty,badge}.{elf,bin,uf2}`,
`irq-F-tests.log`, build/vet logs, CPU disassembly and `irq-F-table-free.patch`.
No native performance benchmark, hardware access, push or source adoption
was performed. Net flash savings do not establish a frame-time improvement:
the predicate, ownership checks and VIC warning counter still need the
normal hardware gate.

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

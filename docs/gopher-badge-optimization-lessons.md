# Optimization lessons: Gopher Badge (Cortex-M0+) hot path

These are lessons learned from measuring several VIC-II hot-path
optimization attempts on real Gopher Badge hardware (see PR #13 and its
sibling attempts). They apply specifically to `cmd/gopher-badge64`
(TinyGo, Cortex-M0+, RP2040): an in-order core with no branch predictor,
no data/instruction cache, and code executing in place from flash (XIP).
Desktop numbers (Ebitengine/amd64/arm64 host builds) do not transfer to
this target and can even point the wrong way.

## Code size / instruction count dominates branch count

On a superscalar, out-of-order host CPU with a branch predictor (e.g. an
Apple M-series or modern x86), replacing predictable branches with
branchless mask/shift arithmetic ("bit tricks") is a classic win: taken
branches risk a pipeline flush, and a predictor exists precisely to
absorb that cost when it can't be removed. That reasoning does not carry
over to the Gopher Badge's Cortex-M0+.

We converted several `bool`-driven decisions in the VIC-II per-dot pixel
path (`dotclock0`..`dotclock7` in `6569.go`) to `uint8` 0x00/0xFF masks
merged with `&`/`&^`/`|` instead of branching. Measured on-device,
steady-state `StepFrame` emulate time per frame (baseline = `main` at the
time, `tinygo build -target=gopher-badge -scheduler=cores`):

| variant | branches removed from `main.main` | instructions added | emulate/frame | vs baseline |
|---|---|---|---|---|
| baseline (`main`) | - | - | 90.59 ms | - |
| full branchless (all border/graphics-bit merges branchless) | -25 | +28 | 101.09 ms | **+11.6%** |
| lighter (only fg/background + main-border merge branchless; vertical border kept as an early-return branch) | fewer | fewer than the full variant | 93.45 ms | **+3.2%** |

Both variants regressed, and the regression size tracked the number of
*added instructions*, not the number of *removed branches*. Desktop
`BenchmarkStepFrame` (Apple M4) showed the same sign (+1.55% for the full
variant) even though an M4 has a predictor and should reward removing
predictable branches - a strong hint that the real cost here was extra
work per dot (shifts, extra ALU ops, wider live ranges), not branch
misprediction, on *either* target.

**Takeaway:** on this target, do not assume "fewer branches" is a proxy
for "faster." A static instruction/branch count comparison of the
compiled `main.main` (the whole frame path inlines into it) is a *useful
sanity check* but not a *substitute* for an on-device timing
measurement - see the next section.

## Static instruction counts on the host toolchain can mislead

Counting instructions/branches in the TinyGo-compiled ELF
(`arm-none-eabi-objdump -d --no-show-raw-insn` + a symbol-scoped count,
or `arm-none-eabi-size` for `.text`) is cheap and worth doing early, but
it is not a reliable performance predictor by itself. It cannot see:

- per-instruction cycle cost on the M0+ (most Thumb ops are 1 cycle, but
  not all, and flash XIP wait states can dominate over the ideal cycle
  count anyway);
- register pressure / spill behaviour introduced by wider live ranges
  (e.g. holding `fg`, `border`, and `color` simultaneously instead of a
  short-lived `bool`);
- how the change interacts with TinyGo's inlining heuristics elsewhere in
  the same hot call chain (a change that looks smaller in isolation can
  push a caller over an inlining threshold and turn it into a real call).

Treat instruction/branch counts as a hypothesis to test, not a
conclusion. The only measurement that actually decided these
optimization attempts was on-device `StepFrame` timing.

## How to get a trustworthy on-device measurement

1. Build and flash with the scheduler TinyGo will actually ship with:
   `tinygo build -target=gopher-badge -scheduler=cores -o out.uf2 ./cmd/gopher-badge64`
   and `tinygo flash -target=gopher-badge -scheduler=cores -port=<serial-port> ./cmd/gopher-badge64`.
   Omitting `-scheduler=cores` measures a different (and not
   representative) build.
2. `cmd/gopher-badge64/main.go` already prints a running average over 50
   frames (`frame %d: emulate=... draw=...`) to the USB serial console -
   read it directly rather than adding new instrumentation:
   `cat /dev/cu.usbmodemXXXX` after `stty -f /dev/cu.usbmodemXXXX 115200 raw -echo cs8 -parenb -cstopb`
   (use the `cu.*` device, not `tty.*` - the latter can block/hang; also
   note the port can report "busy" for a few seconds right after a flash
   completes, so retry rather than assuming failure).
3. Wait for the reported average to stabilize across several consecutive
   lines (the first couple of samples after boot are noisy) before
   trusting the number.
4. Compare against a baseline built and flashed the same way, from the
   same `main` commit the branch is based on, with no other variables
   changed in the same session.
5. Desktop benchmarks (`go test -bench StepFrame`) are still useful as a
   quick, no-hardware-required smoke test and for catching gross
   regressions, but treat their *magnitude* as non-transferable and
   trust their *sign* only as a weak prior - as seen here, both desktop
   and device agreed branchless was slower, but device showed a ~7x
   larger effect.

## Practical implication for future hot-path work on this target

Prefer the smallest, most direct code for a given piece of VIC-II logic.
Don't reach for mask/branchless tricks on this codebase's Cortex-M0+
target speculatively "because it removes branches" - profile the current
`main.main` instruction count as a hypothesis, but confirm with an actual
flashed, running measurement before adopting the change. If a change
adds instructions to remove branches, on this target that is very
unlikely to pay off.

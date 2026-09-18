# tiny64

tiny64 is a Commodore 64 emulator written in Go.

The goal is cycle-accurate emulation, built outward from the VIC-II dot
clock. The dot clock is the one true clock of the machine: the 6510 CPU,
the CIAs, and the VIC-II itself all advance in lock step with it, one dot
at a time. Modelling that clock first, and driving every other chip from
it, is what lets the emulator reproduce timing-sensitive behaviour such as
bad lines, sprite/border timing, and raster interrupts, rather than just
approximating the overall visual result.

The public stepping unit is one bus cycle: `VIC().StepCycle()` advances
eight dots, including the VIC-II's Phi1 accesses, the CPU and CIA Phi2
tick, and then the IEC devices. `StepFrame()` advances exactly one PAL
frame (19,656 cycles) from the current beam position; it does not
synchronize to the top of the frame. Both APIs leave the beam on a cycle
boundary and may be interleaved. The former `StepDot` and `FinishFrame`
APIs have been removed; callers needing frame synchronization can step
cycles until both `VIC().Dot()` and `VIC().RasterLine()` are zero. To
preserve `FinishFrame`'s behavior when already at that position, take at
least one cycle before checking.

### IRQ recognition

The C64's 6510 and the drive's 6502 share a clocked IRQ model, not a delay
measured from an assertion edge. IRQ is an active-low, level-sensitive pin:
each Phi2 samples the combined interrupt sources, including changes caused
by that cycle's peripheral ticks and CPU bus accesses. Instruction polls
use the preceding Phi2 sample. Once accepted, a request survives pin
release until interrupt entry or arbitration in favour of NMI.

Ordinary instructions poll on their final execution cycle. Branches poll
at operand fetch and again on a page-crossing correction, but not on the
last cycle of a taken same-page branch. CLI, SEI and PLP poll with the old
I flag; RTI restores I before its poll. IRQ entry performs seven bus
cycles, including the discarded opcode read, and does not poll again.

RDY holds reads without stopping IRQ sampling. A stretched poll can accept
a pulse during the hold; a stretched non-poll cycle cannot turn that pulse
into a queued interrupt. CLI/SEI update I even while their terminal read
is held, so subsequent repetitions use the new mask. PLP instead waits
for its completing stack read. Writes, including interrupt stack pushes,
continue under the existing BA/AEC contract. NMI/reset recognition and the
VIC's bus scheduling are separate models and are not replaced by this IRQ
implementation.

The 6510 tracks interrupt producers in a source bitmask: VIC and CIA1 drive
IRQ; CIA2 and RESTORE drive NMI. Producers update only their own bits, so
acknowledging one device cannot clear another's request. NMI edge detection
uses the combined NMI level, not individual source transitions.

IRQ source assertions wake a CPU-owned clocking path. It preserves Phi2
sampling and instruction polling until the line and sampled/held candidates
are clear, then becomes inactive. Accepted requests remain latched for
opcode-fetch arbitration even after clocking becomes inactive. CPU reset
discards IRQ history but preserves the peripheral line levels. The drive
6502 separately skips poll classification when no sampled or held candidate
is present.

The shared conformance cases in `irq_test.go` cover both cores, with
additional RDY cases for the C64. Their basis is
[NESdev's interrupt description](https://www.nesdev.org/wiki/CPU_interrupts),
the [Visual6502 recognition stages](https://www.nesdev.org/wiki/Visual6502wiki/6502_Interrupt_Recognition_Stages_and_Tolerances),
and transistor-model RDY schedules, not demo screenshots. Run them with
`go test . -run '^TestIRQ'`.

`TestIRQTransistorReference` in `irq_reference_test.go` preserves 24
original pin schedules and their expected fetch/IRQ and I-flag outcomes
as Go test cases. They were checked against
[Visual6502 revision d8ecc129](https://github.com/trebonian/visual6502/tree/d8ecc129b34e0eaf320e0400fcf33329475bdb1e)
and run offline against the CPU with no external tooling. They verify IRQ
outcomes and the specified
architectural I boundaries, not every bus address during a held read:
the transistor model can update a crossing branch's address while RDY
remains low.

## Scope

tiny64 emulates:

- the 6510 CPU
- the 6569 VIC-II video chip (PAL)
- the 6526 CIA I/O chips
- the serial IEC bus, and a drive on it

## Drives

Two drives can answer for device 8, and **which one is a compile-time
choice**.

By default you get the generic drive: it handles the serial handshake
cycle by cycle, so the unmodified KERNAL cannot tell the difference, but
it implements CBM DOS in Go against a D64 image rather than modelling the
1541's internals. There is no drive CPU to step and no GCR to decode.

Building with `-tags drive1541` replaces it with a complete 1541 - its own
6502 running the real DOS ROM, its two 6522 VIAs, and a rotating GCR track
under the head. That is what software driving the drive's own processor
needs: fastloaders, copy protection, drive-code upload.

    go run ./cmd/c64 -disk demo.d64                     # the generic drive
    go run -tags drive1541 ./cmd/c64 -disk demo.d64     # the real 1541

Both behave identically for `LOAD"$",8`, `LOAD"NAME",8` and `SAVE`.

**The tag is a compile-time choice because the 1541 is not cheap.** The
bus is clocked on every Phi2 cycle, and a 1541 on it means a second 6502
plus two VIAs on every one of a PAL frame's 19,656 cycles -
unconditionally, because the DOS ROM idles in an ATN polling loop and
never sleeps. Measured over the demo fixtures on an M4 Max, against a
machine with nothing on the bus:

| device on the bus | cost per frame |
|---|---|
| the generic drive | +3.9% |
| the full 1541 | +41.7% |

A drive is only plugged in when something asks for one, by inserting a
disk: `cmd/c64` and `cmd/c64cli` both take a `-disk FILE` flag naming a
35-track or 40-track D64 image. Only one drive can answer for a given
address, so `InsertDisk` leaves address 8 alone if something is already
there.

The desktop and headless front ends also have an opt-in `-wedge` flag. It plugs
an 8K autostart cartridge into the expansion port before reset, the way a
fastload or utility cartridge of the period arrived: the KERNAL finds the
`CBM80` signature at `$8004` during its reset sequence and hands the cartridge
control before BASIC has started, and the cartridge initializes the machine
itself and installs a small cassette-buffer dispatcher. Cartridge ROM is
switched in at `$8000-$9FFF` only while servicing wedge commands; BASIC,
the screen editor, KERNAL disk operations, and loaded programs see normal RAM:

    go run ./cmd/c64 -disk demo.d64 -wedge
    go run ./cmd/c64cli -disk demo.d64 -wedge

The cartridge prints `DOS WEDGE ACTIVE` as it starts up, just above the first
`READY.`, and accepts the historical direct-mode DOS Wedge / DOS Manager 5.1
shorthands:

- `$` or `@$` loads and lists the directory without replacing the current BASIC
  program; directory patterns such as `$:DEMO*` are also passed to the drive
- `@` or `>` prints the drive status
- `@S:NAME`, `>S:NAME`, `@N:DISK,ID`, and other `@`/`>` strings are sent to the
  drive's command channel, then the resulting status is printed
- `@#9` selects the active IEC device number for later wedge commands
- `/NAME` expands to `LOAD"NAME",dev`
- `↑NAME` expands to `LOAD"NAME",dev` and runs the program after the load
  returns to the BASIC prompt
- `%NAME` expands to `LOAD"NAME",dev,1` for machine-code programs
- `←NAME` expands to `SAVE"NAME",dev`
- `@Q` removes the prompt hook and switches the cartridge off until hardware
  reset. Neither RESTORE nor RUN/STOP+RESTORE reactivates it. Software
  reactivation with `SYS 32777` is no longer supported: `$8009` is program RAM.

All disk traffic still uses the emulated KERNAL and IEC bus, so the normal
load, save, directory, and command-channel messages remain visible. The wedge
is disabled by default, and when it is enabled it is the cartridge's own 6502
startup code that installs it, as a direct-mode prompt hook. Stored BASIC
program lines are deliberately left to BASIC rather than intercepted by a
CHRGET hook, so wedge tokens in a numbered line retain normal BASIC syntax
behaviour instead of becoming hidden disk operations.

**BASIC reports the normal 38911 bytes free.** The cartridge banks ROM out
while the real KERNAL memory test runs; it neither reserves BASIC memory nor
changes the memory-size result. Its own emulated 6502 firmware installs the
dispatcher, call gate, and workspace at `$033C-$03D1` in the cassette buffer.
No code is injected by the host, and `$8000-$9FFF` and `$C000-$CFFF` remain
available to programs.

Programs do not need a manual cartridge-disable step. For example, with
Uncle Angus McFungus mounted as the first PRG, type `/*`, wait for a successful
load and `READY.`, then type `RUN`. The up-arrow shortcut `↑*` also works.
That shortcut retains its historical queued-`RUN` behavior: it can run the
previous program after a failed LOAD, so use `/NAME` and a separate `RUN`
when you need to check load success first.

The wedge uses a small custom cartridge mapper, not a patched KERNAL or an
FC3 clone. While active, `$DF00-$DFFF` exposes the final 256 bytes of cartridge
ROM through IO2, independently of ROML. Writes to `$DFFF` control a latch:
bit 0 asserts `/EXROM`, and bit 7 releases `/EXROM` and locks out both ROM
windows until hardware reset; other bits are ignored. Thus `$00` hides ROML,
`$01` exposes it, and `$80` switches the cartridge off. Normal CPU-port I/O
banking applies. These registers exist only for the wedge cartridge.
The bootstrap executes in IO2 while RAMTAS clears low RAM; runtime handoffs
execute in cassette RAM or IO2 so they cannot remove their own instructions.
Use `@Q`, not a direct latch POKE, to retire the hook safely.

IRQ is masked during short ROM-only work, with the incoming interrupt state
restored for external calls made with ROML hidden. The cartridge's NMI path
preserves normal RESTORE behavior and hides ROM before a RUN/STOP+RESTORE
warm start abandons the interrupted firmware. Hardware reset reinstalls the
wedge and resets its selected device to 8. The host `EnableDOSWedge` and
`DisableDOSWedge` APIs change the expansion port immediately; call `Reset`
before continuing after either operation.

The cassette buffer and IO2 are still cartridge resources. Software that
overwrites that workspace, takes over cartridge I/O, or installs an NMI
handler that assumes no cartridge is present may need `@Q` first. This is
memory-compatible disk shorthand, not a JiffyDOS fastloader. The
[FC3 hardware description](https://www.c64-wiki.com/wiki/Final_Cartridge_3#The_Hardware)
provides precedent for I/O-controlled GAME/EXROM banking, but the wedge does
not implement its freezer, banking register layout, or firmware.

An optional local regression uses the original Uncle PRG without distributing
it. Run each mode in a fresh process:

    for mode in stock load-run up-arrow; do
        TINY64_UNCLE_ANGUS_PRG=/path/to/uncle-angus_mcfungus.prg \
            go test . -run "^TestDOSWedgeUncleAngus/$mode$" -count=1 -v
    done

It checks the fixture hash, actual KERNAL-loaded bytes, the formerly failing
`$9F9A` read, decompressed memory against the stock result, and 1100 frames
of subsequent execution. The ordinary suite uses original synthetic programs.

## Inspecting programs

`cmd/prg` decodes a `.prg` file - a two byte little endian load address
followed by the bytes loaded there - without running the emulator. With no
flags it prints a header, then works out what the payload is: a BASIC V2
program loaded at `$0801` is listed, and if that program is a `SYS` stub
with machine code behind it the machine code is disassembled too. Anything
else is disassembled from its load address.

    go run ./cmd/prg maze.prg               # header, then a BASIC listing
    go run ./cmd/prg -v game.prg            # addresses in decimal as well
    go run ./cmd/prg -hex sprites.prg       # hex dump instead
    go run ./cmd/prg -disasm -start '$C000' -end '$C0FF' code.prg

A `.prg` stored inside a D64 image can be inspected in place, naming the
file the way CBM DOS does, wildcards and all:

    go run ./cmd/prg -d64 demo.d64 -list    # the disk directory
    go run ./cmd/prg -d64 demo.d64 'MA*'

The disassembler covers all 256 opcodes, including the undocumented ones,
which are printed with a leading `*`.

It is deliberately small: the core package has no dependency on any
graphics library, so it can run headless (for testing) or under
[Ebitengine](https://ebitengine.org/) for a desktop GUI. There is also a
build target for TinyGo, with the long-term aim of running tiny64 on a
Raspberry Pi Pico.

## Layout

- the repository root is the core emulator package (CPU, VIC-II, CIA,
  1541, the generic IEC drive, bus/PLA)
- `rom/` embeds the ROM images the emulator needs to boot
- `cmd/internal/desktop/` is the shared Ebitengine frontend used by the desktop
  commands
- `cmd/c64` is the desktop C64 emulator
- `cmd/c64cli` runs the emulator headless, for testing and debugging
- `cmd/gopher-badge64` is the TinyGo build target for the Gopher Badge
- `cmd/tufty2040` is the TinyGo build target for the Pimoroni Tufty 2040,
  using its parallel ST7789 display through PIO/DMA
- `cmd/drivec` is a standalone 1541 drive/IEC bus test harness, and so is
  built only under `-tags drive1541`
- `cmd/prg` inspects `.prg` files: header, BASIC listing, disassembly
- `cmd/snapshot` captures headless PNGs and checks `testdata/demos` goldens
- `cmd/internal/prg` and `cmd/internal/disasm` are the PRG decoder and the
  6502 disassembler behind it
- `cmd/deadtest` and `cmd/destestmax` run C64 diagnostic cartridges

Tufty 2040 builds should leave TinyGo's default scheduler and optimization
level in place unless re-measuring on hardware; scheduler or `-opt` overrides
can break or regress the build.

    tinygo build -target=tufty2040 -o out.uf2 ./cmd/tufty2040

The Tufty target starts the PIO/DMA panel transfer asynchronously and waits for
it immediately before queuing the next transfer, so most of the display write
overlaps the following emulated frame on the same core.

It also attaches a compact, read-only generic IEC device at address 8. After
the KERNAL reaches the BASIC prompt, the target loads and runs the classic
one-line `PRINT CHR$(205.5+RND(1)); : GOTO 10` maze demo through the normal
IEC and KERNAL `LOAD` path. The compact device serves the PRG directly rather
than allocating a full 175KB D64 image, which would not fit alongside the
Tufty's 320x240 framebuffer in RP2040 RAM.

## The frame buffer

The VIC-II decides on a four bit colour index per pixel. The default Go
frame buffer stores each index in one byte; the desktop GPU expands it
through a palette shader. The `pixelsink_func` build uses the same storage
but calls the pixel writer indirectly, for benchmarking.

`tiny64.C64Palette` holds Pepto's PAL values, derived from the 6569's
colour carrier rather than eyeballed, and identical to the `pepto-pal.vpl`
that VICE ships. Sharing VICE's palette means a capture taken there can be
compared against our output directly, which is how the demo fixtures get
validated against something other than this emulator's own judgement.

Four horizontally adjacent pixels are packed into the RGBA channels of one
texel, including the alpha channel. The 408-pixel rows have a
`FrameBufferStride` of 408 bytes, with no padding, so `FrameBufferIndexed`
can be uploaded directly without CPU row repacking.
The visible picture remains 408x293; the texture is 102x293 and the upload
is 119,544 bytes rather than 478,176 bytes of RGBA. The indexed raster
storage, including non-visible lines, occupies 127,296 bytes.
`cmd/internal/desktop/palette.kage` selects each pixel's channel and looks
up its colour in the palette uniform.

`FrameBufferRGBA` expands a frame for the callers that do want whole
pixels on the CPU -- the tests and `cmd/snapshot` -- into a tightly packed
408x293 RGBA buffer (478,176 bytes).
It returns shared storage: emulation does not update it, but the next call
overwrites it. Copy the result to retain a snapshot across calls.
`FrameBufferIndexed`, in contrast, is a live view of the emulated frame.

TinyGo targets retain their cropped 320x240 RGB565BE frame buffer and
`FrameBufferRGB565BE` API; they do not use the desktop palette shader.
The `headless` sink stores no pixels.

The shader readback tests require a graphics session and run separately
from the ordinary unit tests:

    go test -tags gpu ./cmd/internal/desktop

The DOS wedge, 1541 and IEC drive tests in the root package each boot a
whole emulated C64 and talk to a drive one bus transition at a time, which
is most of the suite's runtime. They are skipped under `-short`:

    go test -short ./...   # fast local run, drive and wedge tests skipped
    go test ./...          # everything, as CI runs it

The physical 1541 is behind a build tag, so `go test ./...` does not
compile it, its GCR codec, its VIAs, or the 6502 core they hang off - nor
the 6502 arm of the IRQ suite, which is the only non-6510 core under test
anywhere. That is 252 test cases the default configuration cannot see, so
CI runs both and so should you before touching drive code:

    go test -tags drive1541 ./...

## Snapshot regression tests

`cmd/snapshot` boots headlessly to `READY.`, injects a PRG directly into RAM,
and types `RUN` for BASIC programs (including BASIC `SYS` stubs). Machine-code
programs start at their load address, or the optional `-start` address. It
writes an exact CPU-expanded C64 palette PNG, without involving the desktop
GPU. Loading bypasses KERNAL/IEC file transfer: these tests cover program
execution and VIC output, not disk loading, fastloaders, or GPU rendering.

```sh
go run ./cmd/snapshot -prg demo.prg -frames 120 -border -o frame-000120.png
go run ./cmd/snapshot -prg demo.prg -start '$2000' -frames 120 -crop -o cropped.png
go test ./cmd/snapshot
go test ./cmd/snapshot -run '^TestSnapshotFixtures$' -count=1
go test ./cmd/snapshot -run '^TestSnapshotFixtures$/^colour-bars$/^frame-000002$' -count=1
```

Fixture regression tests run offline as part of the ordinary unit-test suite.
Each checkpoint executes the already-built test binary in a fresh subprocess,
so emulator globals cannot leak between captures.
Frame counts use `cmd/snapshot`'s `-frames` semantics: frames stepped after
starting the program, including completion of `RUN` key injection for BASIC.
They are not absolute frames since reset.

Fixtures live in `testdata/demos/<lowercase-hyphenated-slug>/`, each with a
`manifest.json`, its PRG, committed reference PNGs, and a provenance README.
The manifest schema is:

```json
{
  "version": 1,
  "program": {
    "file": "demo.prg",
    "sha256": "<64 lowercase hexadecimal characters>"
  },
  "crop": false,
  "start": "$2000",
  "checkpoints": [
    {"frame": 120, "png": "frame-000120.png"}
  ]
}
```

`start` is optional; omit it to preserve BASIC `RUN` or the PRG load address.
It accepts a string containing decimal, `$hex`, or `0xhex`. `crop` is
required and explicit: `false` selects the full 408x293 visible PAL raster;
`true` selects the 320x200 active display. All other fields shown are
required. Checkpoints must be nonempty, with positive, unique frame numbers
and unique PNG filenames. File names must be local basenames with `.prg` or
`.png` extensions. Unknown fields, missing files, and PRG hash mismatches
fail; there are no missing-fixture skips or automatic golden updates.
Compute hashes with `shasum -a 256 testdata/demos/<slug>/<program>.prg`.

Generate references deliberately with `go run ./cmd/snapshot`, matching
the manifest's frame, crop, and optional start settings, then visually
review and commit the PRG and PNGs together. Comparisons decode every opaque
RGB colour to its exact `tiny64.C64Palette` index: PNG compression or palette
entry ordering does not matter, but even one changed pixel, a non-palette
colour, transparency, or wrong dimensions fails. No nearest-colour matching
or tolerance is applied. Failures report the mismatch count and first pixel.
For known colours, the first mismatch also reports expected and actual C64
colour indices. RGBA goldens are validated against the current
`tiny64.C64Palette`; palette changes do not automatically preserve validity
and require explicit review and regeneration of affected goldens.

Set `SNAPSHOT_ARTIFACT_DIR` to an absolute directory to retain
`<slug>/frame-<six-digit-frame>/{expected,actual,diff}.png`. The diff marks
changed pixels magenta and unchanged pixels black (decode/size failures
produce an empty diff). Without this setting the test uses a temporary
directory that Go removes after the test.

The included `colour-bars` program is original test code. Before adding an
external demo, establish permission to redistribute both the PRG and its
captures, and record author, source URL, version, licence or permission,
and any transformations in the fixture README. Public availability alone
does not grant redistribution permission. Tests must never download assets.

When redistribution permission is unconfirmed, keep supplied PRGs and their
fixtures outside the repository. Select an external fixture root explicitly:

```sh
go test ./cmd/snapshot -run '^TestSnapshotFixtures$' -count=1 -args -snapshot-fixtures=/absolute/path/to/demos
```

This replaces the default `testdata/demos` root; it does not supplement it.
Use an absolute path because Go tests run from the package directory.
The external directory has the same layout and manifest schema:
`<root>/<slug>/manifest.json`, the `program.file` PRG, every checkpoint's
`png` file, and `README.md`. Provenance belongs in that freeform README,
not an additional JSON field: record author, source URL, version, licence
or permission status (including unconfirmed redistribution permission),
transformations, and how the reference captures were obtained and reviewed.
The README is documentation, not machine-validated manifest data.

A generated PNG alone cannot execute an integration test: the matching PRG
and its verified SHA-256 are required to reproduce the capture. An absent
root, empty root, missing manifest, missing PRG, or missing checkpoint PNG
fails rather than skipping or falling back to committed fixtures. Do not
approve a new golden simply because changed emulator code generated it;
reference accuracy must be established independently.

## Status

This is a work in progress. See `docs/` for the reference material used
to guide the implementation.

## References

- https://github.com/ice00/jc64/blob/master/src/sw_emulator/hardware/chip/M6569.java
- https://ist.uwaterloo.ca/~schepers/MJK/ascii/vic2-pal.txt
- https://github.com/santatamas/go-c64/blob/master/_resources/roms/README.md
- https://github.com/VICE-Team/svn-mirror/blob/1508c9d55202867ca9843e230f1a984cfefcb350/vice/src/viciisc/vicii-mem.c#L194
- https://github.com/santatamas/go-c64/blob/master/_resources/Doc/6510%20REGISTERS.txt
- https://c64os.com/post/flitiming1
- https://www.cebix.net/VIC-Article.txt
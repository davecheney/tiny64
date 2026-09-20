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

### Autostart

`-autostart FILE` inserts a D64 or a PRG and runs it, the way VICE's
`x64sc -autostart` does. It replaces `-disk` and `-prg` rather than
modifying them, so name at most one of the three:

    go run ./cmd/c64 -autostart demo.d64
    go run ./cmd/c64 -autostart demo.prg
    go run ./cmd/c64cli -autostart demo.prg

Nothing is patched and no ROM is added. It is a prelude that runs before
the front end's frame loop: fast-forward through the KERNAL's boot,
confirm `READY.` is on screen, and put `LOAD"*",8,1` and `RUN` into the
KERNAL's own ten-character type-ahead buffer at `$0277`. The screen
editor, BASIC and the KERNAL then handle them exactly as they would
characters someone typed, and the load happens over the emulated IEC bus
with all its normal messages. Once the prelude returns, the machine is
running the same loop a normal boot runs — nothing stays hooked or armed.

Both commands go into the buffer up front. The `RUN` waits behind the
carriage return that starts the load for as long as the load takes,
because the editor's queue read at `$E5B4` shifts the buffer down one
character at a time and never clears it. That is why there is only one
prompt to find.

`"*"` is CBM DOS's first-file wildcard, so no filename is needed: a PRG
is written as the only file on a fresh disk image, and on a real D64 the
first directory entry is what a demo wants anyway. Two consequences
worth knowing:

- It runs BASIC programs. A machine-code PRG that loads outside `$0801`
  ends up in memory, but `RUN` finds nothing at `$0801` and returns to
  `READY.`; it needs a `SYS`.
- `RUN` is committed before the load finishes, so a failed load is still
  followed by `RUN`, which runs whatever was already in memory.

Autostart happens once, at startup. A reset from inside the emulated
machine does not repeat it — there is no hook to notice with. To
autostart something else, run the program again.

## Cartridges

`-cartridge FILE` plugs a `.crt` cartridge image into the expansion port
before reset. Both front ends take it:

    go run ./cmd/c64 -cartridge testdata/dead_test.crt
    go run ./cmd/c64cli -cartridge testdata/destest-max.crt -cycles 5000000

Nothing about the cartridge is written down in Go. The container's header
says whether the PCB pulls `/EXROM` and `/GAME` low, and each CHIP packet
says where its ROM chip loads, which is what decides whether the image
sits behind `/ROML` or `/ROMH`. tiny64 reads all four from the file.

One thing about that header is easy to get backwards, and a cartridge
mapped into the wrong window runs garbage rather than failing, so it is
worth stating: a line's status byte is **0 when the line is active**, that
is, pulled low. The specification's own worked example is an ordinary 8K
cartridge with `$18 = $00` and `$19 = $01`, and an 8K cartridge asserts
`/EXROM` and leaves `/GAME` floating.

Two wirings are accepted, because they are the two the PLA models:

| cartridge | `/EXROM` | `/GAME` | ROM loads at | chip-select |
|---|---|---|---|---|
| ordinary 8K | asserted | floating | `$8000` | `/ROML` |
| MAX mode | floating | asserted | `$E000` | `/ROMH` |

Both windows are 8K, and the image must be exactly that. Anything else is
refused with an error naming what is in the way: a bank-switching or
freezer cartridge (any hardware type but the generic one), a 16K
cartridge, more than one ROM chip, a chip in a window tiny64 does not
decode, or an image for another Commodore machine.

`-cartridge` cannot be combined with `-autostart`. A cartridge boots
instead of BASIC, and `-autostart` types into a prompt a MAX-mode
cartridge never brings up. A cartridge and a disk together are fine —
that is how the hardware works.

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
graphics library, so it can run headless (for testing) or under SDL3 for a
desktop GUI. It also builds under TinyGo, with the long-term aim of
running tiny64 on a Raspberry Pi Pico. TinyGo is a compiler choice, not a
target: what selects the embedded frame buffer is the `baremetal` build
tag, which TinyGo sets for board targets and not for the host. A host
TinyGo build is a desktop build.

SDL3 is the only desktop backend, and it is the reason both compilers can
build the same frontend: it is reached through a small cgo shim, where
Ebitengine -- which this used to default to -- reaches TinyGo through
purego, whose `func.go` needs `reflect.Value.SetPointer`, and TinyGo's
reflect has no such method.

SDL3 specifically, and not SDL2, for `SDL_SetTexturePalette`: see the
frame buffer section below. It is new in SDL 3.4, so that is the minimum.

The one command that opens a window -- `cmd/c64` -- therefore needs cgo
and the SDL3 development headers:

    sudo apt-get install libsdl3-dev   # Debian, Ubuntu 25.10 and later
    brew install sdl3                  # macOS

Everything else -- the core package, `cmd/c64cli`, `cmd/snapshot`,
`cmd/prg` and the board targets -- builds without either.

The window opens at the largest whole multiple of the 408x293 picture that
leaves room around it on the display, never below 2x. Whole multiples only:
a fractional one makes some emulated dots taller than their neighbours.
Resize it however you like afterwards -- SDL scales the picture to fit,
keeps its proportions and letterboxes the remainder.

## Layout

- the repository root is the core emulator package (CPU, VIC-II, CIA,
  1541, the generic IEC drive, bus/PLA)
- `rom/` embeds the ROM images the emulator needs to boot
- `cmd/internal/desktop/` is the shared SDL3 frontend used by the desktop
  commands
- `cmd/c64` is the desktop C64 emulator
- `cmd/c64cli` runs the emulator headless, for testing and debugging
- `cmd/gopher-badge64` is the Gopher Badge build target, behind
  `-tags gopher_badge`, which TinyGo sets for that board
- `cmd/tufty2040` is the Pimoroni Tufty 2040 build target, using its
  parallel ST7789 display through PIO/DMA, behind `-tags tufty2040`
- `cmd/drivec` is a standalone 1541 drive/IEC bus test harness, and so is
  built only under `-tags drive1541`
- `cmd/prg` inspects `.prg` files: header, BASIC listing, disassembly
- `cmd/snapshot` captures headless PNGs and checks `testdata/demos` goldens
- `cmd/internal/prg` and `cmd/internal/disasm` are the PRG decoder and the
  6502 disassembler behind it

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
frame buffer stores each index in one byte, and that is what the desktop
hands to the GPU: the frame is never expanded to colour on the CPU at all.
The `pixelsink_func` build uses the same storage but calls the pixel
writer indirectly, for benchmarking.

`tiny64.C64Palette` holds Pepto's PAL values, derived from the 6569's
colour carrier rather than eyeballed, and identical to the `pepto-pal.vpl`
that VICE ships. Sharing VICE's palette means a capture taken there can be
compared against our output directly, which is how the demo fixtures get
validated against something other than this emulator's own judgement.

`FrameBufferStride` is 408 bytes, one index per visible dot with no
padding between rows, which is exactly the layout of an `INDEX8` SDL
texture 408 pixels wide. The indexed raster storage, including the
non-visible lines, occupies 127,296 bytes.

The desktop creates that texture, attaches `tiny64.C64Palette` to it with
`SDL_SetTexturePalette`, and each frame copies `FrameBufferIndexed`
straight into the texture's staging buffer. The GPU does the palette
lookup while it draws. So a frame crosses to the GPU as the 119,544 bytes
it actually is rather than the 478,176 bytes of RGBA it expands to, and
the CPU never touches a colour.

This is what the Ebitengine backend used a fragment shader to achieve --
it packed four indices into the channels of one RGBA texel and unpacked
them on the GPU. SDL3 has somewhere to put palette indices directly, so
the same saving needs no shader and no packing.

`FrameBufferRGBA` expands a frame for the callers that do want whole
pixels on the CPU -- the tests and `cmd/snapshot` -- into a tightly packed
408x293 RGBA buffer (478,176 bytes).
It returns shared storage: emulation does not update it, but the next call
overwrites it. Copy the result to retain a snapshot across calls.
`FrameBufferIndexed`, in contrast, is a live view of the emulated frame.

Bare-metal targets -- those TinyGo sets `baremetal` for -- retain their
cropped 320x240 RGB565BE frame buffer and `FrameBufferRGB565BE` API; they
do not use the indexed frame buffer at all. The `headless` sink stores no
pixels.

The 1541 and IEC drive tests in the root package each boot a whole
emulated C64 and talk to a drive one bus transition at a time, which is
most of the suite's runtime. They are skipped under `-short`:

    go test -short ./...   # fast local run, drive tests skipped
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
go test ./cmd/snapshot -run '^TestSnapshotFixtures$/^the-passengers$/^frame-000120$' -count=1
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

Every fixture here is an external demo carrying redistribution permission.
Before adding another, establish permission to redistribute both the PRG and
its captures, and record author, source URL, version, licence or permission,
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
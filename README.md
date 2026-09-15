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

## Scope

tiny64 emulates:

- the 6510 CPU
- the 6569 VIC-II video chip (PAL)
- the 6526 CIA I/O chips
- the 6522 VIA and a complete 1541 disk drive - its own 6502 running the
  real DOS ROM, a rotating GCR track under the head, and the serial IEC
  bus between the two machines - so `LOAD"$",8` reads a real D64, and the
  DOS can format a blank one for itself

There is also a second, much smaller drive that speaks the same wire
protocol without modelling any of the 1541's internals: `AttachVirtualDrive`
puts a device on the bus that handles the serial handshake cycle by cycle
but implements CBM DOS in Go against a D64 image. It has no drive CPU to
step and no GCR to decode, so it costs almost nothing to run, and the
unmodified KERNAL cannot tell the difference - but it cannot run anything
that talks to the drive's own processor.

A drive is only plugged in when something asks for one, with
`AttachDrive`, or by inserting a disk: `cmd/c64` and `cmd/c64cli` both
take a `-disk FILE` flag naming a 35-track or 40-track D64 image, and a `-drive` flag
choosing which drive answers for device 8 — `1541` (the default) or
`virtual`. Only one drive can answer for a given address, so attaching one
replaces the other.

    go run ./cmd/c64 -disk demo.d64                  # the real 1541
    go run ./cmd/c64 -disk demo.d64 -drive=virtual   # the generic drive

Both should behave identically for `LOAD"$",8`, `LOAD"NAME",8` and
`SAVE`; the difference only shows for software that drives the 1541's own
processor.

The desktop and headless front ends also have an opt-in `-wedge` flag. It plugs
an 8K autostart cartridge into the expansion port before reset, the way a
fastload or utility cartridge of the period arrived: the KERNAL finds the
`CBM80` signature at `$8004` during its reset sequence and hands the cartridge
control before BASIC has started, and the cartridge initializes the machine
itself and installs a small cassette-buffer dispatcher. Cartridge ROM is
switched in at `$8000-$9FFF` only while servicing wedge commands; BASIC,
the screen editor, KERNAL disk operations, and loaded programs see normal RAM:

    go run ./cmd/c64 -disk demo.d64 -wedge
    go run ./cmd/c64cli -disk demo.d64 -drive=virtual -wedge

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
Uncle Agnus McFungus mounted as the first PRG, type `/*`, wait for a successful
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
        TINY64_UNCLE_AGNUS_PRG=/path/to/uncle-angus_mcfungus.prg \
            go test . -run "^TestDOSWedgeUncleAgnus/$mode$" -count=1 -v
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
- `cmd/drivec` is a standalone 1541 drive/IEC bus test harness
- `cmd/prg` inspects `.prg` files: header, BASIC listing, disassembly
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

Four horizontally adjacent pixels are packed into the RGBA channels of one
texel, including the alpha channel. The 405-pixel rows have a
`FrameBufferStride` of 408 bytes, with three unused padding bytes, so
`FrameBufferIndexed` can be uploaded directly without CPU row repacking.
The visible picture remains 405x284; the texture is 102x284 and the upload
is 115,872 bytes rather than 460,080 bytes of RGBA. The indexed raster
storage, including non-visible lines, occupies 127,296 bytes.
`cmd/internal/desktop/palette.kage` selects each pixel's channel and looks
up its colour in the palette uniform.

`FrameBufferRGBA` expands a frame for the callers that do want whole
pixels on the CPU -- the tests and `cmd/snapshot` -- into a tightly packed
405x284 RGBA buffer (460,080 bytes), excluding the indexed row padding.
It returns shared storage: emulation does not update it, but the next call
overwrites it. Copy the result to retain a snapshot across calls.
`FrameBufferIndexed`, in contrast, is a live view of the emulated frame.

TinyGo targets retain their cropped 320x240 RGB565BE frame buffer and
`FrameBufferRGB565BE` API; they do not use the desktop palette shader.
The `headless` sink stores no pixels.

The shader readback tests require a graphics session and run separately
from the ordinary unit tests:

    go test -tags gpu ./cmd/internal/desktop

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
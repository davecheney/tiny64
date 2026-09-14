# tiny64

tiny64 is a Commodore 64 emulator written in Go.

The goal is cycle-accurate emulation, built outward from the VIC-II dot
clock. The dot clock is the one true clock of the machine: the 6510 CPU,
the CIAs, and the VIC-II itself all advance in lock step with it, one dot
at a time. Modelling that clock first, and driving every other chip from
it, is what lets the emulator reproduce timing-sensitive behaviour such as
bad lines, sprite/border timing, and raster interrupts, rather than just
approximating the overall visual result.

## Scope

tiny64 emulates:

- the 6510 CPU
- the 6569 VIC-II video chip (PAL)
- the 6526 CIA I/O chips
- a virtual IEC drive that speaks the real serial bus protocol
  (`AttachVirtualDrive`) cycle by cycle, but implements CBM DOS directly
  in Go against a D64 image rather than emulating a drive's own CPU. It
  has no drive CPU to step and no GCR to decode, so it costs almost
  nothing to run, and the unmodified KERNAL cannot tell the difference -
  but it cannot run anything that talks to a real 1541's own processor.

  This is the `tinygo` branch: it targets microcontrollers, where the
  real 1541 CPU/VIA/GCR emulation main carries is too costly to run
  cycle-accurately. It intentionally does not have that emulation; see
  main for cycle-accurate 1541 support.

A drive is only plugged in when something asks for one, with
`AttachVirtualDrive`, or by inserting a disk: `cmd/c64` and `cmd/c64cli`
both take a `-disk FILE` flag naming a 35-track or 40-track D64 image,
which attaches the virtual drive automatically.

    go run ./cmd/c64 -disk demo.d64

The desktop and headless front ends also have an opt-in `-wedge` flag. It plugs
an 8K autostart cartridge into the expansion port before reset, the way a
fastload or utility cartridge of the period arrived: the KERNAL finds the
`CBM80` signature at `$8004` during its reset sequence and hands the cartridge
control before BASIC has started, and the cartridge initializes the machine
itself and points BASIC's main-loop vector at a handler that runs from ROM at
`$8000-$9FFF`:

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
- `@Q` deactivates the wedge; `SYS 32777` reactivates it, through a `JMP` at a
  fixed offset in the cartridge header

All disk traffic still uses the emulated KERNAL and IEC bus, so the normal
load, save, directory, and command-channel messages remain visible. The wedge
is disabled by default, and when it is enabled it is the cartridge's own 6502
startup code that installs it, as a direct-mode prompt hook. Stored BASIC
program lines are deliberately left to BASIC rather than intercepted by a
CHRGET hook, so wedge tokens in a numbered line retain normal BASIC syntax
behaviour instead of becoming hidden disk operations.

Nothing is copied into RAM, so `$C000-$CFFF` stays free; the wedge's few bytes
of state live in the cassette buffer at `$033C`. Two things follow from the
cartridge being real rather than simulated: BASIC reports 30719 bytes free
instead of 38911, because the KERNAL's memory test walks up from `$0400` and
finds the cartridge sitting where RAM would be, and a program that banks the
cartridge out by clearing LORAM banks the wedge out with it.

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

- the repository root is the core emulator package (CPU, VIC-II, CIA, the
  virtual IEC drive, bus/PLA)
- `rom/` embeds the ROM images the emulator needs to boot
- `cmd/internal/desktop/` is the shared Ebitengine frontend used by the desktop
  commands
- `cmd/c64` is the desktop C64 emulator
- `cmd/c64cli` runs the emulator headless, for testing and debugging
- `cmd/gopher-badge64` is the TinyGo build target for the Gopher Badge
- `cmd/tufty2040` is the TinyGo build target for the Pimoroni Tufty 2040,
  using its parallel ST7789 display through PIO/DMA
- `cmd/prg` inspects `.prg` files: header, BASIC listing, disassembly
- `cmd/internal/prg` and `cmd/internal/disasm` are the PRG decoder and the
  6502 disassembler behind it
- `cmd/deadtest` and `cmd/destestmax` run C64 diagnostic cartridges

Both `cmd/tufty2040` and `cmd/gopher-badge64` print a per-50-frame line to
their USB serial console with emulate/draw timing and the RP2040 XIP
(execute-in-place flash) cache's hit rate over that window, read from
`XIP_CTRL.CTR_HIT`/`CTR_ACC`. Use this to tell a real cache-pressure
regression (hit rate dropping) apart from any other cause of a frame-time
change.

Measured on a Tufty 2040 against the `tinygo` branch point: the default
`-opt=z` (size) build ran emulate≈79ms/frame; `-opt=2` (speed) ran
emulate≈73-74ms/frame, a ~7% win, for roughly 45KB more flash (108KB →
156KB) but no measurable RAM cost. XIP cache hit rate was ≈99.97-100% at
both optimization levels, ruling out flash-cache pressure as the
difference; the win is from `-opt=2` generating faster code outright.
`-opt=2` is therefore the recommended flag for on-device measurement
unless flash space becomes tight enough to need `-opt=z` back. Tufty 2040
measurements must use `-scheduler=none`: that target has no goroutines.
The Gopher Badge target has a render goroutine and must instead use
`-scheduler=cores`; scheduler settings are target-specific and must not be
interchanged.

    tinygo build -target=tufty2040 -opt=2 -scheduler=none -o out.uf2 ./cmd/tufty2040

The Tufty target starts the PIO/DMA panel transfer asynchronously and waits for
it immediately before queuing the next transfer, so most of the display write
overlaps the following emulated frame on the same core.

It also attaches a compact, read-only generic IEC device at address 8. After
the KERNAL reaches the BASIC prompt, the target loads and runs the classic
one-line `PRINT CHR$(205.5+RND(1)); : GOTO 10` maze demo through the normal
IEC and KERNAL `LOAD` path. The compact device serves the PRG directly rather
than allocating a full 175KB D64 image, which would not fit alongside the
Tufty's 320x240 framebuffer in RP2040 RAM.

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
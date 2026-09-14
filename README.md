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
itself and points BASIC's main-loop vector at its handler:

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
- `@Q` deactivates the wedge; `SYS 828` reactivates it, through the resident
  stub and a `JMP` at a fixed offset in the cartridge header

All disk traffic still uses the emulated KERNAL and IEC bus, so the normal
load, save, directory, and command-channel messages remain visible. The wedge
is disabled by default, and when it is enabled it is the cartridge's own 6502
startup code that installs it, as a direct-mode prompt hook. Stored BASIC
program lines are deliberately left to BASIC rather than intercepted by a
CHRGET hook, so wedge tokens in a numbered line retain normal BASIC syntax
behaviour instead of becoming hidden disk operations.

The cartridge takes itself out of the CPU's map once it has started up, the
way the utility cartridges of the period did: the PCB carries a bank-control
latch in the expansion port's I/O1 window at `$DE00`, and writing bit 0 there
releases `/EXROM` and leaves the RAM underneath `$8000-$9FFF` visible. Code
cannot bank out the ROM it is executing from, so the instructions that work the
latch live in RAM - 50 bytes in the cassette buffer at `$033C`, which BASIC's
`$0302` vector points at. They read the input line with the cartridge still
gone, bank it in, call into it through a fixed `JMP` in its header, and bank it
out again before BASIC touches the line. The handler returns with the carry
saying whether BASIC should interpret the line or go straight to `READY.`.

So the ROM is only in the CPU's map while wedge code is actually running in it,
and BASIC reports the full 38911 bytes free. That figure is not arranged
anywhere in Go: the cartridge copies a second, throwaway stub to the screen
page - the one piece of RAM the KERNAL's `RAMTAS` does not clear - and calls
`RAMTAS` from there with itself banked out, so the memory walk that sets the
top of BASIC memory finds RAM all the way up to `$A000`. `$C000-$CFFF` stays
free too; the wedge's only RAM is that stub and a few bytes of state after it
in the cassette buffer.

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
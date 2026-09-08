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
take a `-disk FILE` flag naming a 35-track D64 image. Only one drive can
answer for a given address, so attaching one replaces the other.

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
- `cmd/tiny64` is the TinyGo build target for embedded hardware
- `cmd/drivec` is a standalone 1541 drive/IEC bus test harness
- `cmd/deadtest` and `cmd/destestmax` run C64 diagnostic cartridges

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
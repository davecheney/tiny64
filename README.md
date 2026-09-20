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

RESTORE uses a direct NMI press event rather than a Phi2-clocked pulse.
Front ends must call `Keyboard.Restore` once per press edge. A press can
trigger NMI while CIA2 holds its NMI line asserted; pulse width and the
hardware's combined-line suppression are not modeled. CIA2 sampling and
the lightweight CPU's existing IRQ handling are otherwise unchanged.
This is an independent adaptation of #57, without #54 or #56.

TinyGo services interrupts at the next unstalled opcode fetch, without
synchronization delays or I-flag pipelining. IRQ uses CIA1's level and the
current I flag; NMI retains priority and edge-latched CIA2/RESTORE behavior.
There are no interrupt timestamps or countdowns to wrap. This deliberate
accuracy tradeoff is for character-mode workloads; use `main` for precise
interrupt/VIC timing. The exported `CPU.Clock` field is removed. External code
reading `GetCPU().Clock` must track elapsed cycles at its stepping boundary.
The Tufty demo retains its three-frame key transitions using frame delays.

The VIC-II scheduler clocks CPU, CIAs, then IEC once per
bus cycle. `CPU.TickPhi2` advances only the CPU; callers needing the whole
machine should use `VICII.StepCycle` or `StepFrame`. CIA underflows are
visible to the CPU on a subsequent fetch, not earlier in the same cycle.

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
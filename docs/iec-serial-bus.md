# The Commodore IEC serial bus

Notes gathered while implementing the two IEC devices in this tree: the full
1541 (`1541.go`, `6502.go`, `6522.go`, `d64_drive1541.go`,
`iec_drive1541.go`, all built only under `-tags drive1541`) and the
generic drive, which is what answers for device 8 by default
(`iecdevice.go`, `cbmdos.go`, `d64fs.go`).

The published documentation on this bus is unusually contradictory - the two
most-cited disassemblies label the line-setting routines backwards, and the
one paragraph everybody quotes for the turnaround describes the wrong edge.
Everything below has been checked against a primary source, and where the
sources disagree the disagreement is called out rather than silently
resolved. Facts that were established by disassembling the ROMs in
`rom/` are marked as such.

## Sources

1. Jim Butterfield, "How the VIC/64 Serial Bus Works", *Compute!* July 1983 —
   <http://www.zimmers.net/anonftp/pub/cbm/programming/serial-bus.txt>
2. J. Derogee, "IEC disected", 2008 —
   <http://www.zimmers.net/anonftp/pub/cbm/programming/serial-bus.pdf>
   (embeds Butterfield as pages 4-8 and adds the canonical timing table on
   page 11)
3. pagetable.com, "Commodore Peripheral Bus, Part 4: Standard Serial (IEC)" —
   <https://www.pagetable.com/?p=1135>
4. pagetable.com, "Part 2: The TALK/LISTEN Layer" — <https://www.pagetable.com/?p=1031>
5. 1541 DOS 2.6 ROM disassembly — <http://www.ffd2.com/fridge/docs/1541dis.html>
6. C64 KERNAL disassembly — <http://www.ffd2.com/fridge/docs/c64-diss.html>
7. VICE — `vice/src/iecbus/iecbus.c`, `vice/src/c64/c64iec.c`,
   `vice/src/drive/iec/via1d1541.c`

> **Do not trust the ffd2 KERNAL comments for `$EE85`, `$EE8E`, `$EE97` and
> `$EEA0`.** Their English labels are inverted: they describe the register bit
> being set, not the state of the line. Derogee's function list (p.19-20) has
> them the right way round, and so does the ROM itself.

## 1. Electrical model

Three open-collector lines - ATN, CLOCK, DATA - plus ground and SRQ (unused
on the C64). Open-collector means the bus is a wired-AND of every device's
contribution: a line is *asserted* (electrically low) if **any** device pulls
it, and *released* (high, via a pull-up) only when **every** device has let
go. No device can drive a line high.

Throughout this codebase "asserted" means the electrical low, and a
peripheral only ever says whether it is pulling. That is what the
`iecPeripheral` interface in `iec.go` expresses: `iecCLKOut`/`iecDATAOut`
return "am I pulling this line", and `CLKAsserted`/`DATAAsserted` fold the
C64's contribution together with every attached device's.

### 1a. Register polarity

This is the part the documentation gets wrong most often, so it was verified
directly against the KERNAL ROM bytes in `rom/`:

| Address | Bytes | Meaning |
|---|---|---|
| `$ED2E` | `AD 00 DD 09 08 8D 00 DD` | `ORA #$08` → **ATN asserted** |
| `$EDBE` | `... 29 F7 ...` | `AND #$F7` → ATN released |
| `$EE85` | `... 29 EF ...` | `AND #$EF` → CLOCK **released** |
| `$EE8E` | `... 09 10 ...` | `ORA #$10` → CLOCK **asserted** |
| `$EE97` | `... 29 DF ...` | `AND #$DF` → DATA released |
| `$EEA0` | `... 09 20 ...` | `ORA #$20` → DATA asserted |

So for **CIA2 port A (`$DD00`) on writes, 1 = asserted**. There are inverting
7406 buffers between the CIA pins and the bus, so a 1 in the register pulls
the line low. VICE agrees: `iec_update_cpu_bus()` is handed `~$DD00`.

Inputs are **not** inverted:

* bit 6 = CLOCK IN, bit 7 = DATA IN, **0 = asserted**.

Hence `$EEA9`:

```
EEA9  AD 00 DD   LDA $DD00
EEAC  CD 00 DD   CMP $DD00
EEAF  D0 F8      BNE $EEA9     ; debounce: re-read until two reads agree
EEB1  0A         ASL           ; C = DATA IN, N = CLOCK IN
EEB2  60         RTS
```

Everything else about `$DD00` is an ordinary port bit and must read back what
was written, DDR-effective. The KERNAL relies on this: every line change is a
read-modify-write (`LDA $DD00 / ORA #$10 / STA $DD00`), so returning the
inverted bus state for bits 3-5 silently corrupts the other two lines. See
`cia2ReadPRA` in `iec.go`.

The 1541 side (VIA1 `$1800`) uses the opposite convention on reads and the
same on writes:

```
Bit 7   ATN IN            Bit 3   CLOCK OUT
Bits 6-5 device address   Bit 2   CLOCK IN
Bit 4   ATN acknowledge   Bit 1   DATA OUT
                          Bit 0   DATA IN
```

Reads: 1 = line asserted. Writes: 1 = pull low.

### 1b. The 1541's hardware ATN acknowledge

A device must answer ATN within 1000 µs even if its CPU is busy, so the
answer is wired, not programmed. On the 1541, DATA is forced low by hardware
whenever ATN is asserted and ATNA (`$1800` bit 4) is clear. To release DATA
during an ATN phase the DOS must first *set* ATNA (`$E873`), and it must
*clear* it again when ATN goes away (`$E8DB`) or DATA would be stuck low
forever.

The generic drive has no such gate - it simply pulls DATA in `beginATN()` on
the same cycle it sees ATN fall, which is well inside T_AT.

## 2. The ATN sequence

The C64 is always the controller. To address the bus it asserts ATN and sends
one command byte using the ordinary byte protocol, with itself as talker.
Every device on the bus must:

1. pull DATA within **T_AT = 1000 µs** (device-not-present error otherwise),
2. release CLOCK, since it is now a listener,
3. receive the byte, and
4. decide whether the byte concerns it.

Command bytes:

| Range | Meaning |
|---|---|
| `$20`+addr | LISTEN |
| `$3F` | UNLISTEN (all devices) |
| `$40`+addr | TALK |
| `$5F` | UNTALK (all devices) |
| `$60`+ch | secondary address: select channel (DATA) |
| `$E0`+ch | secondary address: CLOSE |
| `$F0`+ch | secondary address: OPEN |

Bit 4 of a secondary address is ignored, so `$6F` and `$7F` are both channel
15. Secondary addresses are accepted whether or not the device is the
addressed one - it is the LISTEN/TALK flags that gate the data phase, which
is why `OPEN 6,6` reports no error on a bus with no device 6.

ATN aborts everything unconditionally. The 1541 resets its stack at `$E85B`
and clears its LISTEN/TALK flags on every ATN, which means that by the time
the terminating UNLISTEN arrives those flags are already false. A device
therefore cannot gate its end-of-command handling on "was I listening?" - it
has to remember at the DOS layer that something is outstanding. This is a
real trap; see `cbmDOS.pending` in `cbmdos.go`.

### The ATN race

When ATN falls, CLOCK is usually still *released* from the previous
transaction, because the C64 asserts ATN at `$ED2E` and only pulls CLOCK a
few cycles later at `$ED37`. A device that starts looking for the byte
protocol immediately will read that stale released CLOCK as "talker ready to
send" and desynchronise.

A real 1541 gets away with it by accident: interrupt latency plus the stack
reset take longer than the C64 needs to get to `$ED37`. A device implemented
in software has to wait for CLOCK to be asserted before starting - state
`iecAtnWaitCLK` in `iecdevice.go`.

## 3. Byte transfer

Bits go **LSB first**. DATA released = 1, DATA asserted = 0.

Initial state: talker holds CLOCK, listener holds DATA.

1. Talker releases CLOCK — "ready to send".
2. Listener releases DATA — "ready for data". It may take as long as it likes
   (listener hold-off, T_H, is unbounded).
3. For each of 8 bits: talker asserts CLOCK, puts the bit on DATA, waits
   T_S, then releases CLOCK. **The bit is valid on the CLOCK release**, and
   is held for T_V.
4. After the 8th bit the listener pulls DATA within **T_F = 1000 µs** — the
   frame handshake. If it does not, the talker reports a frame error.

## 4. EOI

There is no ninth bit and no length field. To mark the last byte the talker
simply *does not* pull CLOCK at step 1 above. The listener times out after
200 µs, concludes EOI, and answers with a DATA pulse (asserted for T_EI, at
least 80 µs when the listener is a peripheral). Then the transfer proceeds
normally.

The C64's thresholds as listener, measured by CIA1 timer B: **≤256 µs = more
data, 256-512 µs = EOI, >512 µs = empty stream**.

Two consequences worth knowing:

* **File-not-found has no error code on the wire.** The drive releases both
  lines after the turnaround and lets the C64's `ACPTR` time out twice, which
  sets ST and produces `?FILE NOT FOUND`.
* Conversely, **a first CLOCK pulse that is too short reads as a spurious
  EOI**, because the timer that synthesises EOI is the same one that covers
  the wait for the talker's first CLOCK assertion. See §7.

## 5. Turnaround

After `TALK` + secondary the C64 must hand the bus over. At that instant the
logic is the wrong way round: the device is holding DATA (it was listening)
and the computer is holding CLOCK (it was talking).

`TKSA` (`$EDC7`) is definitive:

```
EDC7  STA $95            ; secondary address
EDC9  JSR $ED36          ; send it, ATN still asserted
EDCC  SEI
EDCD  JSR $EEA0          ; DATA asserted   <- computer becomes the listener
EDD0  JSR $EDBE          ; ATN released
EDD3  JSR $EE85          ; CLOCK released
EDD6  JSR $EEA9
EDD9  BMI $EDD6          ; spin until the device pulls CLOCK
EDDB  CLI
EDDC  RTS
```

The device answers by releasing DATA and then asserting CLOCK.

Two things to note. First, Butterfield says the device waits for "the Clock
line go true"; that is a slip, the trigger is the computer *releasing* CLOCK.
pagetable Part 4 has it right. Second, **there is no timeout in the `$EDD6`
loop** - if the device never pulls CLOCK the KERNAL hangs forever. There is
no way to detect a missing talker.

Relevant timings: T_TK (ATN release to device taking CLOCK) 20-100 µs, T_DA
(how long the device then holds CLOCK before starting a byte) at least 80 µs.
The hold matters: it is what guarantees the C64's `$EE1B` loop sees CLOCK
asserted at all.

## 6. The timing table

Verbatim from Derogee, "IEC disected" p.11.

| Description | Symbol | Min | Typ | Max |
|---|---|---|---|---|
| ATN response (required)¹ | T_AT | – | – | 1000 µs |
| Listener hold-off | T_H | 0 | – | infinite |
| Non-EOI response to RFD² | T_NE | – | 40 µs | 200 µs |
| Bit set-up talker⁴ | T_S | 20 µs | 70 µs | – |
| Data valid | T_V | 20 µs | 20 µs | – |
| Frame handshake³ | T_F | 0 | 20 µs | 1000 µs |
| Frame to release of ATN | T_R | 20 µs | – | – |
| Between bytes time | T_BB | 100 µs | – | – |
| EOI response time | T_YE | 200 µs | 250 µs | – |
| EOI response hold time⁵ | T_EI | 60 µs | – | – |
| Talker response limit | T_RY | 0 | 30 µs | 60 µs |
| Byte-acknowledge⁴ | T_PR | 20 µs | 30 µs | – |
| Talk-attention release | T_TK | 20 µs | 30 µs | 100 µs |
| Talk-attention acknowledge | T_DC | 0 | – | – |
| Talk-attention ack. hold | T_DA | 80 µs | – | – |
| EOI acknowledge | T_FR | 60 µs | – | – |

> Notes:
> 1: If maximum time exceeded, device not present error.
> 2: If maximum time exceeded, EOI response required.
> 3: If maximum time exceeded, frame error.
> 4: T_V and T_PR minimum must be 60 µs for external device to be a talker.
> 5: T_EI minimum must be 80 µs for external device to be a listener.

Notes 4 and 5 are the two rules that bind a peripheral specifically. pagetable
Part 4 explains why note 4 exists:

> "the system architecture of the C64 had the video chip halt the CPU for
> about 40 µs every ~500 µs, which would make it likely for the CPU to miss
> the 20 µs window in which a bit was valid."

So the protocol was changed to require 60 µs hold times from *devices*, while
*controllers* were still allowed 20 µs. That asymmetry is the reason a device
implementation cannot simply mirror the C64's own timing.

## 7. `ACPTR`, and why 60 µs is not enough

`ACPTR` (`$EE13`) is the KERNAL routine that receives one byte. Disassembled
from `rom/`:

```
EE13  78         SEI
EE14  A9 00      LDA #$00
EE16  85 A5      STA $A5        ; EOI-seen flag
EE18  20 85 EE   JSR $EE85      ; release CLOCK
EE1B  20 A9 EE   JSR $EEA9
EE1E  10 FB      BPL $EE1B      ; wait for the talker to RELEASE CLOCK
EE20  A9 01      LDA #$01
EE22  8D 07 DC   STA $DC07      ; CIA1 timer B high = 1  -> 256 us
EE25  A9 19      LDA #$19
EE27  8D 0F DC   STA $DC0F      ; start it, one-shot
EE2A  20 97 EE   JSR $EE97      ; release DATA  -> "ready for data"
EE2D  AD 0D DC   LDA $DC0D      ; clear ICR
EE30  AD 0D DC   LDA $DC0D      ; <-- loop: timer expired?
EE33  29 02      AND #$02
EE35  D0 07      BNE $EE3E      ;     yes -> EOI path
EE37  20 A9 EE   JSR $EEA9
EE3A  30 F4      BMI $EE30      ;     CLOCK still released -> keep waiting
EE3C  10 18      BPL $EE56      ;     CLOCK asserted -> read the bits
EE3E  A5 A5      LDA $A5
EE40  F0 05      BEQ $EE47
EE42  A9 02      LDA #$02
EE44  4C B2 ED   JMP $EDB2      ; second timeout -> ST |= $02, give up
EE47  20 A0 EE   JSR $EEA0      ; EOI ack: assert DATA
EE4A  20 85 EE   JSR $EE85      ;         release CLOCK
EE4D  A9 40      LDA #$40
EE4F  20 1C FE   JSR $FE1C      ; ST |= $40
EE52  E6 A5      INC $A5
EE54  D0 CA      BNE $EE20      ; rearm the timer and go round again
EE56  A9 08      LDA #$08
EE58  85 A5      STA $A5        ; 8 bits to go
EE5A  AD 00 DD   LDA $DD00      ; <-- wait for CLOCK RELEASED
EE5D  CD 00 DD   CMP $DD00
EE60  D0 F8      BNE $EE5A
EE62  0A         ASL            ; C = DATA IN, N = CLOCK IN
EE63  10 F5      BPL $EE5A
EE65  66 A4      ROR $A4        ; shift the bit in
EE67  AD 00 DD   LDA $DD00      ; <-- wait for CLOCK ASSERTED
EE6A  CD 00 DD   CMP $DD00
EE6D  D0 F8      BNE $EE67
EE6F  0A         ASL
EE70  30 F5      BMI $EE67
EE72  C6 A5      DEC $A5
EE74  D0 E4      BNE $EE5A      ; next bit
EE76  20 A0 EE   JSR $EEA0      ; frame handshake: assert DATA
EE79  24 90      BIT $90
EE7B  50 03      BVC $EE80
EE7D  20 06 EE   JSR $EE06
EE80  A5 A4      LDA $A4
EE82  58         CLI
EE83  18         CLC
EE84  60         RTS
```

Three things follow that are not obvious from the timing table.

**The 256 µs timer at `$EE20` is armed once per byte and covers only the wait
for the talker's *first* CLOCK assertion** (the `$EE30` loop). Expiring there
is what synthesises EOI. So a first CLOCK pulse the C64 fails to observe is
not reported as a timing error - it is reported as `?FILE NOT FOUND`, because
LOAD sees ST bit 6 set while reading the two-byte load address. The mirror
image on the drive side is the 1541's own EOI timer, armed at `$E9DA`
(`STA $1805`, T1 = `$0100`).

**Every wait is level-triggered, not edge-triggered.** `$EE30`, `$EE5A` and
`$EE67` all spin until the line *is* in the wanted state. Nothing latches an
edge, so a pulse that begins and ends between two consecutive samples is not
merely late - it is invisible.

**The worst-case gap between two samples is about 75 cycles.** The `$EE5A`
and `$EE67` loops are roughly 15 cycles a pass; the `$EE30` loop is about 40.
Each read is debounced by a second read (`LDA` / `CMP` / `BNE`), which costs
an extra pass whenever it straddles a transition. And the VIC-II can stall
the CPU for up to 43 cycles on a Bad Line.

The consequence is that **both** halves of a bit have to be held longer than
that - not just T_V, as note 4 implies, but T_S too. And the failure mode is
vicious. If `ACPTR` misses a CLOCK *assertion* it stays in the `$EE67` loop
and is then satisfied by the *next* bit's assertion, so it silently drops one
bit and hangs forever waiting for a ninth that never comes. Nothing on screen
says so: `SEARCHING` and `LOADING` have already been printed.

Observed here with T_S at the table minimum of 20 µs and T_V at the note-4
figure of 60 µs: an intermittent stall 5 to 11 bytes into a 14-byte file, the
exact point moving with raster phase. `iecdevice.go` uses 110 cycles for both
phases, comfortably clear of the sampling gap, giving about 570 bytes/second
- still quicker than a real 1541.

This is why `TestDriveLoadsFileThroughKERNAL` and `TestLoadFile` both repeat
the load at several raster phases and compare bytes rather than the screen. A
transfer that only works at one phase works by luck.

## 8. KERNAL routine reference

| Address | Routine |
|---|---|
| `$ED09` | `TALK` — send `$40`+device under ATN |
| `$ED0C` | `LISTEN` — send `$20`+device under ATN |
| `$ED2E` | assert ATN |
| `$ED36` | send a byte under ATN |
| `$ED40` | send a byte (the bit loop; note the four `NOP`s at `$ED80`) |
| `$EDB9` | `SECOND` — secondary address after LISTEN |
| `$EDBE` | release ATN |
| `$EDC7` | `TKSA` — secondary address after TALK, then turnaround |
| `$EDDD` | `CIOUT` — send a data byte (buffers one byte so the last can carry EOI) |
| `$EDEF` | `UNTLK` |
| `$EDFE` | `UNLSN` |
| `$EE13` | `ACPTR` — receive a byte |
| `$EE85` | release CLOCK |
| `$EE8E` | assert CLOCK |
| `$EE97` | release DATA |
| `$EEA0` | assert DATA |
| `$EEA9` | debounced read: C = DATA IN, N = CLOCK IN |

One behaviour of `CIOUT` is worth calling out because it contradicts the
protocol documents: **the C64 marks the end of a filename with EOI**, not
merely with the following UNLISTEN. `CIOUT` transmits one byte behind so that
the last one can be flagged. A device must therefore treat *either* EOI on
the data stream *or* UNLISTEN as the end of a command or filename, and must
be idempotent about it, because both will arrive.

## 9. How this maps onto the code

`iec.go` owns the bus. `iecPeripheral` is the interface a device implements;
`iecBus` is the list of attached devices; `ATNAsserted`, `CLKAsserted` and
`DATAAsserted` compute the wired-AND across the C64 and every device.
`attachIEC` replaces a device of the same concrete type rather than adding a
second, so two drives cannot end up fighting over the lines at one address.

`iecTick()` is called from `6569.go` by `stepCycle`, the one function that
produces a system Phi2, immediately after `cpu.TickPhi2()`. That is deliberately
the machine clock rather than `CPU.TickPhi2` itself, which unit tests also call
directly.

Two devices implement the interface:

* `drive1541` — the real thing. Its bus lines come from VIA1 port B and its
  `iecTick` steps the drive's 6502. ATN reaches it through an inverter into
  VIA1 CA1, which is what raises the DOS's ATN interrupt.
* `iecDevice` — the generic drive. A state machine that drives the lines
  directly, with `cbmDOS` above it and `d64fs.go` below. No drive CPU, no
  VIAs, no GCR, no head. Because the part that faces the C64 is real, the
  unmodified KERNAL cannot tell the difference; because nothing below the
  wire is modelled, it cannot run anything that talks to the drive's own CPU.

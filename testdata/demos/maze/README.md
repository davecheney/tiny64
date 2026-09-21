# Maze

This fixture is the one-line BASIC maze, the canonical C64 type-in:

```basic
10 PRINT CHR$(205.5+RND(1));:GOTO 10
```

It prints one of the two diagonal PETSCII glyphs at random, for ever, and
900 frames is long enough to fill the screen with them.

- Author: folklore; printed in the Commodore 64 User's Guide and in
  countless magazines since
- Cached program: `maze.prg`
- SHA-256: `30a5d131cbb4a2fabbeed556ff5fd9340d0a7d26f459d358cf7ba8fa0c8be616`

It is here for a different reason from the other four fixtures. They are
demos, and every one of them uses sprites, a bitmap mode, or both, which
is what makes them worth having: they are the hardest thing the emulator
does. It also means that a build made with `-tags vicmini` - the cut-down
VIC-II the microcontroller targets use, with no sprite unit and standard
character mode alone - cannot run any of them, and so had no visual
regression coverage at all.

This one it can run, because there is nothing in it but characters. The
same manifest is checked by both configurations, so the golden below is
what holds them to each other: the subset either paints the screen the
full build paints, to the pixel, or the fixture test says so.

That makes it the weakest fixture as a demo and the most useful one as a
control. It exercises the character sequencer, the video matrix and colour
RAM, the border on all four sides and the background, and nothing else -
which is exactly the set `vicmini` keeps.

It is also what the Tufty 2040 runs: `cmd/tufty2040/assets/maze.prg` is
the same program, and the board's `maze=` telemetry reports whether it
loaded. The frame times quoted for that board are this workload.

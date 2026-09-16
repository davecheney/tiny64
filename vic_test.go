package tiny64

import "testing"

// stepFrame runs the VIC-II for exactly one full PAL frame.
func stepFrame(v *VICII) {
	v.StepFrame()
}

// TestVICResetIsIdle checks that Reset() puts the video logic in idle
// state with VC/VCBase/RC/VMLI all cleared (section 3.7.1).
func TestVICResetIsIdle(t *testing.T) {
	v := &VICII{}
	v.control1 = 0xFF // DEN set, to prove Reset doesn't derive idle from registers
	v.RC = 5
	v.VC = 123
	v.Reset()

	if !v.idle {
		t.Errorf("idle = false, want true after Reset")
	}
	if v.RC != 0 || v.VC != 0 || v.VCBase != 0 || v.VMLI != 0 {
		t.Errorf("RC=%d VC=%d VCBase=%d VMLI=%d, want all 0 after Reset", v.RC, v.VC, v.VCBase, v.VMLI)
	}
}

func TestVICRegisterStorage(t *testing.T) {
	v := VICII{rasterCompare: 0x1FF}
	for reg := uint16(0); reg < 0x2F; reg++ {
		v.WriteRegister(0xD000+reg, uint8(reg+1))
	}

	for reg := uint16(0); reg < 0x2F; reg++ {
		switch reg {
		case 0x11:
			if got, want := v.ReadRegister(0xD000+reg), uint8(reg+1)&0x7F; got != want {
				t.Errorf("register $%02X = $%02X, want $%02X", reg, got, want)
			}
		case 0x12:
			if got := v.ReadRegister(0xD000 + reg); got != 0 {
				t.Errorf("live raster register = $%02X, want $00", got)
			}
		case 0x19:
			if got, want := v.ReadRegister(0xD000+reg), uint8(0x70); got != want {
				t.Errorf("register $%02X = $%02X, want $%02X", reg, got, want)
			}
		case 0x1A:
			if got, want := v.ReadRegister(0xD000+reg), uint8(0xFB); got != want {
				t.Errorf("register $%02X = $%02X, want $%02X", reg, got, want)
			}
		case 0x1E, 0x1F:
			if got := v.ReadRegister(0xD000 + reg); got != 0 {
				t.Errorf("collision register $%02X = $%02X, want $00", reg, got)
			}
		default:
			if got, want := v.ReadRegister(0xD000+reg), uint8(reg+1); got != want {
				t.Errorf("register $%02X = $%02X, want $%02X", reg, got, want)
			}
		}
	}

	if got := v.ReadRegister(0xD02F); got != 0xFF {
		t.Errorf("unimplemented register $2F = $%02X, want $FF", got)
	}
}

// TestVICStaysIdleWithoutDEN is a regression test: without DEN ever being
// set, there is never a Bad Line Condition, so the video logic must stay
// in idle state and RC/VC/VCBase must stay at 0 forever (previously, RC
// incremented unconditionally every line regardless of state, counted up
// to 7 and got stuck there, and VCBase/VC grew without bound).
func TestVICStaysIdleWithoutDEN(t *testing.T) {
	quietMachine(t)
	v := &VICII{}
	v.Reset()

	for range 3 {
		stepFrame(v)
	}

	if !v.idle {
		t.Errorf("idle = false, want true (DEN was never set, so no Bad Line should ever occur)")
	}
	if v.RC != 0 {
		t.Errorf("RC = %d, want 0 (must not free-run when there's no Bad Line Condition)", v.RC)
	}
	if v.VC != 0 || v.VCBase != 0 {
		t.Errorf("VC=%d VCBase=%d, want both 0 (must not advance in idle state)", v.VC, v.VCBase)
	}
}

// TestVICBadLineEntersDisplayState checks that setting DEN produces a Bad
// Line Condition at raster $33 (with YSCROLL=3), which flips the video
// logic into display state and starts advancing RC; that Bad Lines recur
// every 8 raster lines (once per text row) so RC correctly cycles 0-7
// repeatedly instead of getting stuck; and that the video logic returns
// to idle state once raster lines stop matching YSCROLL (i.e. past $F7,
// where no further Bad Line Condition can occur).
func TestVICBadLineEntersDisplayState(t *testing.T) {
	quietMachine(t)
	v := &VICII{}
	v.Reset()
	v.control1 = 0x13 // DEN=1, YSCROLL=3, RSEL=0

	// Steps into the next raster line, far enough that its first phi0low
	// (four dots into the cycle) has run and evaluated the Bad Line
	// Condition. That is true as soon as the line's first whole cycle
	// has been stepped.
	stepLine := func() {
		startLine := v.rasterLine
		for v.rasterLine == startLine || v.dot < DotsPerCycle {
			v.StepCycle()
		}
	}

	// Run up to (but not including) raster line $33, the first line whose
	// low 3 bits match YSCROLL=3 once allowBadLine has latched (which
	// happens at the end of raster line $30, per section 3.5).
	for v.rasterLine != 0x32 || v.dot != 0 {
		v.StepCycle()
	}
	if !v.idle {
		t.Fatalf("idle = false before raster $33, want true")
	}

	// Raster line $33 is a Bad Line (YSCROLL=3 matches its low 3 bits, and
	// DEN was set during raster line $30), so the video logic should enter
	// display state, and RC should cycle 0-7 on each subsequent Bad Line's
	// row instead of running away or getting stuck.
	stepLine() // process raster $33
	rcSeen := map[uint8]bool{}
	for range 25 * 8 {
		if v.idle {
			t.Fatalf("idle = true at raster=%d, want false (Bad Lines recur every 8 lines through $30-$F7)", v.rasterLine)
		}
		if v.RC > 7 {
			t.Fatalf("RC = %d at raster=%d, want 0-7", v.RC, v.rasterLine)
		}
		rcSeen[v.RC] = true
		stepLine()
	}
	for rc := uint8(0); rc <= 7; rc++ {
		if !rcSeen[rc] {
			t.Errorf("RC never took the value %d across 25 text rows", rc)
		}
	}

	// Once raster lines no longer match YSCROLL within $30-$F7 (i.e. past
	// $F7), no more Bad Lines occur and the video logic must return to
	// idle state instead of staying stuck in display state forever.
	for v.rasterLine <= badLineRasterEnd {
		v.StepCycle()
	}
	if !v.idle {
		t.Errorf("idle = false past raster $F7, want true (no more Bad Lines can occur)")
	}
}

// TestVICGAccessCountPerRow is a regression test: a Bad Line's row must
// produce exactly 40 g-accesses (one per column), so VC advances by
// exactly 40 per row and every column (including the last) gets a g-access
// loading fresh graphics data. Previously g-access stopped one cycle too
// early, leaving the 40th column's gdSequencer stale (displaying as blank)
// and VC drifting out of sync with the video matrix every row.
func TestVICGAccessCountPerRow(t *testing.T) {
	quietMachine(t)
	v := &VICII{}
	v.Reset()
	v.control1 = 0x1B // DEN=1, RSEL=1, YSCROLL=3

	// Run to the start of the first Bad Line's row ($33).
	for v.rasterLine != 0x33 || v.dot != 0 {
		v.StepCycle()
	}
	vcBefore := v.VC
	for v.rasterLine == 0x33 {
		v.StepCycle()
	}
	if got := v.VC - vcBefore; got != 40 {
		t.Errorf("VC advanced by %d across one Bad Line row, want 40", got)
	}
}

// TestVICVideoMatrixAddress checks that a Bad Line's c-access reads from
// the video matrix address derived from $D018's VM bits (VM<<10 + VC),
// and that VC stays within the 1000-entry video matrix range across a
// full frame instead of drifting to unrelated pages of memory.
func TestVICVideoMatrixAddress(t *testing.T) {
	quietMachine(t)
	v := &VICII{}
	v.Reset()
	v.control1 = 0x1B    // DEN=1, RSEL=1, YSCROLL=3 (KERNAL defaults)
	v.memPointers = 0x14 // VM=1 (screen at $0400), CB=2 (chars at $1000)

	// Mark every page of RAM with its own page number, so any c-access
	// landing outside the expected $0400-$07E7 video matrix range is
	// immediately obvious from the value read.
	for page := range 256 {
		for i := range 256 {
			ram[page*256+i] = byte(page)
		}
	}
	parkCPU() // the fill above wrote over the parked loop

	maxVC := uint16(0)
	for range CyclesPerFrame {
		v.StepCycle()
		if v.VC > maxVC {
			maxVC = v.VC
		}
	}

	const videoMatrixSize = 1000 // 40 columns * 25 rows
	if maxVC > videoMatrixSize {
		t.Errorf("VC reached %d during a frame, want <= %d (video matrix is only 1000 entries)", maxVC, videoMatrixSize)
	}
}

func TestVICRasterIRQ(t *testing.T) {
	quietMachine(t)
	v := &VICII{}
	v.Reset()

	// Set raster compare target to line 50.
	v.WriteRegister(0xD012, 50)
	// Enable raster IRQ ($D01A bit 0).
	v.WriteRegister(0xD01A, 0x01)

	// Step lines until line 50 is reached.
	for v.rasterLine != 50 {
		v.StepCycle()
	}

	if !v.IRQ {
		t.Errorf("v.IRQ = false at line 50, want true")
	}
	if got := v.ReadRegister(0xD019); got != 0xF1 {
		t.Errorf("ReadRegister($D019) = $%02X, want $F1", got)
	}

	// Acknowledge IRQ by writing 1 to bit 0 of $D019.
	v.WriteRegister(0xD019, 0x01)

	if v.IRQ {
		t.Errorf("v.IRQ = true after acknowledge, want false")
	}
	if got := v.ReadRegister(0xD019); got != 0x70 {
		t.Errorf("ReadRegister($D019) = $%02X, want $70", got)
	}
}

func TestVICRasterIRQTriggersWhenCompareIsWrittenOnCurrentLine(t *testing.T) {
	t.Run("$D012 low byte", func(t *testing.T) {
		v := &VICII{}
		v.Reset()
		v.rasterLine = 1
		v.rasterCompare = 2
		v.interruptEnable = 0x01

		v.WriteRegister(0xD012, 1)

		if !v.IRQ {
			t.Fatal("raster IRQ not triggered when $D012 was written to the current line")
		}
		if v.interruptStatus&0x01 == 0 {
			t.Fatal("raster IRQ status not latched after same-line $D012 write")
		}

		v.WriteRegister(0xD019, 0x01)
		v.WriteRegister(0xD012, 2)
		v.WriteRegister(0xD012, 1)
		if v.IRQ {
			t.Fatal("raster IRQ retriggered more than once on the same raster line")
		}

		v.WriteRegister(0xD012, 2)
		v.dot = DotsPerLine - 1
		v.dotclock7(v.reloadDot())
		if !v.IRQ {
			t.Fatal("raster IRQ did not trigger after advancing to a new raster line")
		}
	})

	t.Run("$D011 high bit", func(t *testing.T) {
		v := &VICII{}
		v.Reset()
		v.rasterLine = 0x100
		v.rasterCompare = 0
		v.interruptEnable = 0x01

		v.WriteRegister(0xD011, 0x80)

		if !v.IRQ {
			t.Fatal("raster IRQ not triggered when $D011 was written to the current line")
		}
	})
}

func TestVICRasterLineZeroIRQTriggersInCycleTwo(t *testing.T) {
	quietMachine(t)
	v := &VICII{}
	v.Reset()
	v.rasterLine = RasterLinesPerFrame - 1
	// The last whole cycle of the last line of the frame, so the cycle
	// stepped below is the one that wraps the beam to line 0 - cycle 1 of
	// the new frame.
	v.dot = DotsPerLine - DotsPerCycle

	v.StepCycle()
	if v.rasterLine != 0 || v.dot != 0 {
		t.Fatalf("dot=%d raster=%d after the wrapping cycle, want the top of the frame", v.dot, v.rasterLine)
	}
	if v.interruptStatus&0x01 != 0 {
		t.Fatal("line-zero raster IRQ triggered in cycle 1")
	}

	v.StepCycle()
	if v.dot != DotsPerCycle {
		t.Fatalf("dot=%d after cycle 2, want %d", v.dot, DotsPerCycle)
	}
	if v.interruptStatus&0x01 == 0 {
		t.Fatal("line-zero raster IRQ did not trigger in cycle 2")
	}
}

func TestVICRasterIRQReachesCPU(t *testing.T) {
	saveMachine(t)

	ram = [65536]byte{}
	bus = Bus{}
	cpu = CPU{}
	cpu.PortDDR = 0xFF // plain RAM everywhere, so IRQ vector is test-controlled
	cpu.SP = 0xFF
	cia1 = CIA{}
	cia2 = CIA{}
	keyboard = Keyboard{}
	vic = VICII{}
	vic.Reset()

	copy(ram[0x0200:], []uint8{0x4C, 0x00, 0x02}) // JMP $0200
	copy(ram[0x0400:], []uint8{
		0xE6, 0x10, // INC $10
		0x40, // RTI
	})
	ram[0xFFFE], ram[0xFFFF] = 0x00, 0x04
	cpu.PC = 0x0200

	vic.WriteRegister(0xD012, 2)
	vic.WriteRegister(0xD01A, 0x01)

	for i := 0; i < CyclesPerLine*4 && ram[0x0010] == 0; i++ {
		vic.StepCycle()
	}

	if ram[0x0010] == 0 {
		t.Fatal("CPU did not service VIC raster IRQ")
	}
}

// TestVICCounterWrapsAtTenBits states the width of VC directly. Section
// 3.7.2 of the VIC Article calls it "a 10-bit counter", so advancing it
// off $3FF wraps to zero rather than reaching $400.
func TestVICCounterWrapsAtTenBits(t *testing.T) {
	for _, tc := range []struct {
		from, want uint16
	}{
		// The counter runs past the 1000 bytes of visible video matrix -
		// see TestVICCAccessReadsTheInvisibleTailOfTheMatrix - and turns
		// over only at the end of its ten bits.
		{999, 1000},
		{1023, 0},
	} {
		v := &VICII{}
		v.Reset()
		v.idle = false // a g-access only advances VC in display state
		v.VC = tc.from

		v.cycleGAccess()

		if v.VC != tc.want {
			t.Errorf("VC = %d after advancing off %d, want %d: VC is ten bits wide", v.VC, tc.from, tc.want)
		}
	}
}

// TestVICLinecrunchKeepsCountersInRange drives section 3.14.4's
// "Linecrunch". Negating the Bad Line Condition inside a Bad Line that has
// already begun leaves the sequencer in display state with RC untouched,
// so RC reaches 7 and stays there, and cycle 58 loads VCBASE from a VC
// that the line's forty g-accesses have advanced. Doing that on every
// raster line walks VCBASE up by 40 a line, and the article says where it
// ends up:
//
//	"This eventually makes VCBASE cross the 1000 byte limit of the video
//	matrix and the VIC will display the last, normally invisible, 24 bytes
//	of the matrix (where also the sprite data pointers are stored). VCBASE
//	wraps around to zero when reaching 1024."
//
// It is a technique rather than a fault - it scrolls the screen upwards
// by whole text lines without moving any graphics memory - so the counters
// have to survive it. Before VC was masked they did not: it ran past $3FF
// within a hundred lines, and the next c-access indexed colour RAM, which
// is exactly 1024 bytes, out of range.
//
// The crunch stops part way down the screen so that ordinary Bad Lines
// follow it, which is what puts a c-access behind a counter that has
// wrapped - the read that used to panic.
func TestVICLinecrunchKeepsCountersInRange(t *testing.T) {
	saveMachine(t)
	newMachine(t)
	iecBus = nil

	vic.memPointers = 0x14 // screen $0400, chars $1000
	cia2.PRA, cia2.DDRA = 3, 3

	// Crunch the top half of the display window ($30-$F7) and leave the
	// rest to run normally.
	const crunchUntilLine = 0x90

	crunched, badLinesAfter := 0, 0
	for cycle := range CyclesPerFrame {
		switch {
		case vic.rasterLine < crunchUntilLine:
			switch vic.dot / DotsPerCycle {
			case 0:
				// Open the line with DEN set and YSCROLL matching the
				// beam, so this line's first phi0low sees a Bad Line
				// Condition and takes the sequencer out of idle state.
				vic.WriteRegister(0xD011, 0x10|uint8(vic.rasterLine)&0x07)
			case 1:
				// Negate it again before cycle 12, the first of the three
				// cycles that reload VC from VCBASE and, on a Bad Line,
				// clear RC. Aborting later than this still resets RC,
				// which is what keeps a normal screen's rows in step and
				// is exactly what Linecrunch must avoid.
				if vic.badLine {
					crunched++
				}
				vic.WriteRegister(0xD011, 0x10|(uint8(vic.rasterLine)+1)&0x07)
			}
		case vic.badLine:
			badLinesAfter++
		}

		vic.StepCycle()

		if vic.VC > 0x3FF || vic.VCBase > 0x3FF {
			t.Fatalf("cycle %d (raster %d): VC=%#x VCBase=%#x, want both inside ten bits: they are ten bit counters and wrap at 1024",
				cycle, vic.rasterLine, vic.VC, vic.VCBase)
		}
	}

	// A frame that crunched nothing, or that never went back to fetching
	// afterwards, would satisfy the check above without having tested it.
	if crunched == 0 {
		t.Error("no Bad Line was aborted, so nothing was crunched and VC was never driven past the video matrix")
	}
	if badLinesAfter == 0 {
		t.Error("no Bad Line ran after the crunch, so no c-access read colour RAM behind a wrapped counter")
	}
}

// TestVICCAccessReadsTheInvisibleTailOfTheMatrix pins the rest of what
// section 3.14.4 describes. A crunched screen drives VC through 1000-1023,
// past the 40x25 characters anyone can see, and the article says what the
// VIC fetches there: "the VIC will display the last, normally invisible,
// 24 bytes of the matrix (where also the sprite data pointers are
// stored)".
//
// So those offsets are read, not clamped away or skipped. It is worth
// stating separately from the wrap, because clamping VC at 1000 would
// also stop it running off the end of colour RAM and would look like a
// fix.
func TestVICCAccessReadsTheInvisibleTailOfTheMatrix(t *testing.T) {
	saveMachine(t)
	newMachine(t)

	vic.memPointers = 0x14 // video matrix at $0400
	cia2.PRA, cia2.DDRA = 3, 3

	// Offset 1000 is the first byte past the visible 40x25, and 1016 is
	// where sprite 0's data pointer lives.
	ram[0x0400+1000], colorRAM[1000] = 0x5A, 0x0B
	ram[0x0400+1016], colorRAM[1016] = 0xA5, 0x0C

	for _, tc := range []struct {
		vc   uint16
		want uint16
	}{
		{1000, 0x0B5A},
		{1016, 0x0CA5},
	} {
		vic.VC, vic.VMLI = tc.vc, 0
		// A real Bad Line spends three cycles warning the CPU off the bus
		// before its first c-access, so by the time one runs the warning
		// has expired. Calling cycleCAccess directly skips that, so say so.
		vic.baLowCycles = baWarningCycles + 1
		vic.cycleCAccess()
		if got := vic.videoMatrixColor[0]; got != tc.want {
			t.Errorf("c-access at VC=%d read %#04x, want %#04x (colour nibble in the high byte)", tc.vc, got, tc.want)
		}
	}
}

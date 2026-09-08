// The Commodore serial (IEC) bus from the controller's side, which on a
// real machine is the C64's KERNAL bit-banging CIA2's port A. drivec needs
// its own copy so it can talk to the drive with no C64 in the picture.
//
// The three open-collector lines are ATN, CLK and DATA. A byte moves like
// this, with the talker driving CLK and the listener driving DATA:
//
//   - the listener holds DATA asserted to say "I'm here";
//   - the talker releases CLK to say "ready to send";
//   - the listener releases DATA to say "ready to receive";
//   - for each of 8 bits, LSB first, the talker asserts CLK, puts the bit
//     on DATA (asserted = 0, released = 1), then releases CLK - the
//     listener samples DATA on that release;
//   - the listener asserts DATA to acknowledge the byte.
//
// The drive's side of exactly this dance is at $E9C9 (receive) and $E909
// (send) in the 1541 DOS ROM.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/davecheney/tiny64"
)

// One drive Phi2 cycle is one microsecond, so these are both cycle counts
// and microseconds.
//
// settle is how long each half of a bit is held. The floor is set by the
// drive, not by us: its receive loop reads the bus through a debounced
// double read and only comes round about every 60us, so a shorter pulse
// than that can pass unseen between two samples. The bus has no upper
// bound on how slow a talker may be, so there is no cost to being
// comfortably above it.
//
// timeout is how long to wait for the drive to move a handshake on before
// giving up, and eoiDelay is the "nothing more is coming" pause that marks
// the last byte of a transfer - it has to sit above the drive's own 256us
// EOI timer without being so long that a normal byte trips it.
const (
	settle   = 100
	timeout  = 2_000_000
	eoiDelay = 400
)

// errTimeout means the drive stopped taking part in the handshake, which
// on a real bus is what DEVICE NOT PRESENT is made of.
var errTimeout = errors.New("timed out waiting for the drive")

// bus is the controller end of the IEC bus. Every wait it does runs the
// drive, since the drive only moves when its CPU is clocked.
type bus struct {
	cycles int64
	last   lines
}

// trace, set by DRIVEC_TRACE, logs every change of the bus lines together
// with the drive's PC, which is how a handshake that stalls gets pinned
// down to the edge it was waiting for.
var trace = os.Getenv("DRIVEC_TRACE") != ""

// tracePC logs every instruction the drive executes, which is heavy but
// is the only way to see which branch a stalled handshake took.
var tracePC = os.Getenv("DRIVEC_TRACE_PC") != ""

type lines struct{ atn, clk, data bool }

func (b *bus) sample() lines {
	return lines{tiny64.ATNAsserted(), tiny64.CLKAsserted(), tiny64.DATAAsserted()}
}

func (b *bus) traceLines(where string) {
	if !trace {
		return
	}
	s := b.sample()
	if s == b.last && where == "" {
		return
	}
	b.last = s
	fmt.Fprintf(os.Stderr, "%9d atn=%v clk=%v data=%v drv=%04X %s\n",
		b.cycles, s.atn, s.clk, s.data, tiny64.GetDriveCPU().PC, where)
}

// tick runs the drive for n of its Phi2 cycles.
func (b *bus) tick(n int) {
	cpu := tiny64.GetDriveCPU()
	for range n {
		cpu.TickPhi2()
		b.cycles++
		if tracePC && cpu.TState == 1 {
			fmt.Fprintf(os.Stderr, "%9d PC=%04X OP=%02X A=%02X X=%02X Y=%02X $7D=%02X\n",
				b.cycles, cpu.PC-1, cpu.Opcode, cpu.A, cpu.X, cpu.Y, tiny64.DriveRAM()[0x7D])
		}
		b.traceLines("")
	}
}

// wait runs the drive until cond holds, giving up after timeout cycles.
func (b *bus) wait(cond func() bool) error {
	for range timeout {
		if cond() {
			return nil
		}
		b.tick(1)
	}
	return errTimeout
}

// waitUpTo runs the drive until cond holds or n cycles have passed,
// reporting whether it happened. Unlike wait, running out is a normal
// outcome: it's how the bus signals EOI.
func (b *bus) waitUpTo(cond func() bool, n int) bool {
	for range n {
		if cond() {
			return true
		}
		b.tick(1)
	}
	return cond()
}

func (b *bus) clkAsserted() bool  { return tiny64.CLKAsserted() }
func (b *bus) dataAsserted() bool { return tiny64.DATAAsserted() }

// send clocks one byte out to whichever devices are listening. atn selects
// a command byte (device numbers, channel numbers) rather than data, and
// eoi marks the last byte of a data transfer.
func (b *bus) send(v byte, atn, eoi bool) error {
	tiny64.SetCIA2ATN(atn)
	tiny64.SetCIA2DATA(false) // the listener owns DATA
	tiny64.SetCIA2CLK(true)
	b.traceLines(fmt.Sprintf("send %#02x atn=%v eoi=%v", v, atn, eoi))
	b.tick(settle)

	// The listener holds DATA down until it is ready for the byte.
	if err := b.wait(b.dataAsserted); err != nil {
		return fmt.Errorf("no device answered: %w", err)
	}
	tiny64.SetCIA2CLK(false) // ready to send
	if err := b.wait(func() bool { return !b.dataAsserted() }); err != nil {
		return fmt.Errorf("listener never became ready: %w", err)
	}

	if eoi {
		// Saying nothing for longer than the listener's EOI timer is what
		// marks the end of the transfer; it answers with a DATA pulse.
		if !b.waitUpTo(b.dataAsserted, eoiDelay*4) {
			return fmt.Errorf("listener did not acknowledge EOI: %w", errTimeout)
		}
		if err := b.wait(func() bool { return !b.dataAsserted() }); err != nil {
			return fmt.Errorf("listener held DATA after EOI: %w", err)
		}
	}

	for bit := range 8 {
		b.traceLines(fmt.Sprintf("bit %d", bit))
		tiny64.SetCIA2CLK(true)
		tiny64.SetCIA2DATA(v&1 == 0) // a 0 bit is DATA asserted
		v >>= 1
		b.tick(settle)
		tiny64.SetCIA2CLK(false) // the listener samples on this edge
		b.tick(settle)
	}

	tiny64.SetCIA2CLK(true)
	tiny64.SetCIA2DATA(false)
	b.traceLines("await ack")
	if err := b.wait(b.dataAsserted); err != nil {
		return fmt.Errorf("listener did not acknowledge the byte: %w", err)
	}
	return nil
}

// receive clocks one byte in from the talking device, reporting whether it
// was flagged as the last one.
func (b *bus) receive() (v byte, eoi bool, err error) {
	tiny64.SetCIA2CLK(false) // the talker owns CLK
	tiny64.SetCIA2DATA(true) // and we hold DATA down: not ready yet

	// Wait for the talker to take CLK before saying anything. Releasing
	// DATA any earlier would be read as the EOI acknowledgement the drive
	// looks for at $E91C, and both ends would then sit waiting for the
	// other.
	if err := b.wait(b.clkAsserted); err != nil {
		return 0, false, fmt.Errorf("talker never took the bus: %w", err)
	}
	if err := b.wait(func() bool { return !b.clkAsserted() }); err != nil {
		return 0, false, fmt.Errorf("talker never became ready: %w", err)
	}
	tiny64.SetCIA2DATA(false) // ready for data

	// If the talker doesn't start clocking within the EOI window, this is
	// the last byte, and it wants a DATA pulse back before it sends it.
	if !b.waitUpTo(b.clkAsserted, eoiDelay) {
		eoi = true
		tiny64.SetCIA2DATA(true)
		b.tick(60)
		tiny64.SetCIA2DATA(false)
		if err := b.wait(b.clkAsserted); err != nil {
			return 0, true, fmt.Errorf("talker did not resume after EOI: %w", err)
		}
	}

	for i := range 8 {
		if err := b.wait(func() bool { return !b.clkAsserted() }); err != nil {
			return 0, eoi, fmt.Errorf("bit %d never arrived: %w", i, err)
		}
		if !b.dataAsserted() {
			v |= 1 << uint(i)
		}
		if err := b.wait(b.clkAsserted); err != nil {
			return 0, eoi, fmt.Errorf("bit %d never ended: %w", i, err)
		}
	}

	tiny64.SetCIA2DATA(true) // acknowledge
	b.tick(settle)
	return v, eoi, nil
}

// listen tells device dev to listen on channel sa, so that everything sent
// afterwards lands in that channel.
func (b *bus) listen(dev, sa byte) error {
	if err := b.send(0x20|dev, true, false); err != nil {
		return err
	}
	if err := b.send(0xF0|sa, true, false); err != nil {
		return err
	}
	tiny64.SetCIA2ATN(false)
	b.tick(settle)
	return nil
}

// unlisten ends the transfer. The 1541 executes a command channel's
// contents when it sees this, so it's what actually sets a command going.
func (b *bus) unlisten() error {
	err := b.send(0x3F, true, false)
	tiny64.SetCIA2ATN(false)
	b.tick(settle)
	return err
}

// talk tells device dev to send from channel sa, then performs the
// turnaround that hands CLK to the drive and leaves us holding DATA.
func (b *bus) talk(dev, sa byte) error {
	if err := b.send(0x40|dev, true, false); err != nil {
		return err
	}
	if err := b.send(0x60|sa, true, false); err != nil {
		return err
	}
	tiny64.SetCIA2DATA(true)
	tiny64.SetCIA2CLK(false)
	tiny64.SetCIA2ATN(false)
	b.tick(settle)
	return nil
}

// untalk stops the talking device.
func (b *bus) untalk() error {
	tiny64.SetCIA2CLK(true)
	tiny64.SetCIA2DATA(false)
	err := b.send(0x5F, true, false)
	tiny64.SetCIA2ATN(false)
	tiny64.SetCIA2CLK(false)
	b.tick(settle)
	return err
}

// command opens channel sa on device dev, sends s, and closes the
// transfer, which is what makes the drive act on it.
func (b *bus) command(dev, sa byte, s string) error {
	if err := b.listen(dev, sa); err != nil {
		return err
	}
	for i := range len(s) {
		if err := b.send(s[i], false, i == len(s)-1); err != nil {
			return err
		}
	}
	return b.unlisten()
}

// read pulls everything device dev has to say on channel sa, up to max
// bytes.
func (b *bus) read(dev, sa byte, max int) ([]byte, error) {
	if err := b.talk(dev, sa); err != nil {
		return nil, err
	}
	var out []byte
	for len(out) < max {
		v, eoi, err := b.receive()
		if err != nil {
			return out, err
		}
		out = append(out, v)
		if eoi {
			break
		}
	}
	return out, b.untalk()
}

// idle runs the drive with the bus at rest, for the stretches where it is
// off doing something mechanical - a format takes about twenty seconds of
// drive time, none of which involves the bus.
func (b *bus) idle(cycles int) {
	tiny64.SetCIA2ATN(false)
	tiny64.SetCIA2CLK(false)
	tiny64.SetCIA2DATA(false)
	b.tick(cycles)
}

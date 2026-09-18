package tiny64

// iecDevice is a disk drive that speaks the Commodore serial bus protocol
// directly, rather than by running a 1541's DOS ROM on an emulated 6502.
// It is a real bus citizen - it bit-bangs ATN/CLOCK/DATA with the same
// handshakes and timings a 1541 does, so the unmodified KERNAL serial
// routines drive it and no ROM patching is involved - but everything above
// the wire (channels, files, the directory) is handled in Go by cbmDOS,
// and everything below it (GCR, the read head, the stepper) doesn't exist.
//
// What that buys: LOAD, SAVE, the directory, and the command channel all
// work, at real bus speed. What it costs: no fastloader, no copy
// protection, no drive code upload, no timing-dependent trickery. This
// branch does not model the full 1541 (GCR/6502/VIA) at all, so this is
// the only drive available here.
//
// The protocol implemented here is the standard CBM serial bus, following
// Butterfield's "How the VIC/64 Serial Bus Works" and the CBM timing table
// (T_AT, T_NE, T_V, T_YE, T_EI, T_BB and friends) as reproduced in
// Derogee's "IEC disected". Every wait below cites the step it implements.
type iecDevice struct {
	address uint8 // primary device address, normally 8

	// clk and data are what this device is doing to the bus: true means
	// "pulling the line low". The bus is open-collector, so that is the
	// only thing a device gets to say (see iecPeripheral).
	clk, data bool

	state iecState
	timer int // Phi2 cycles spent in the current state, ~1us each

	// Byte in flight, LSB first, plus the bit counter walking it.
	shift uint8
	bit   int
	eoi   bool

	// Bus-level addressing state, updated by command bytes seen under
	// ATN. atn is true from the ATN falling edge until it is released.
	atn       bool
	listening bool
	talking   bool
	secondary uint8 // last secondary address byte seen ($6x/$Ex/$Fx)

	dos cbmDOS
}

// iecState enumerates the device's position in the serial protocol. Most
// states are "wait for a line to move", since the bus is a handshake all
// the way down; the few that aren't are timed delays.
type iecState uint8

const (
	// iecIdle covers both "no ATN, not addressed" and "addressed to
	// somebody else, waiting for ATN to go away".
	iecIdle iecState = iota

	// iecAtnWaitCLK is the gap between the ATN falling edge and the
	// controller taking over the CLOCK line.
	iecAtnWaitCLK

	// Receiving a byte, as listener. Used both for command bytes under
	// ATN and for data bytes after ATN is released.
	iecRecvReady    // step 1: wait for talker's "ready to send"
	iecRecvWaitCLK  // step 2: released DATA, wait for CLK or an EOI timeout
	iecRecvEOIHold  // EOI acknowledge pulse, DATA held low
	iecRecvEOIWait  // wait for the talker to resume after our EOI ack
	iecRecvBitValid // step 3: wait for CLK release, sample the bit
	iecRecvBitDone  // wait for CLK to be pulled again

	// Talk-attention turnaround, then sending bytes as talker.
	iecTurnaround // T_TK: ATN released, about to take over CLK
	iecSendHold   // T_DA: hold CLK low so the C64 sees the turnaround
	iecSendReady  // step 1: fetch a byte and offer it
	iecSendWaitRFD
	iecSendWaitEOIAck // listener's EOI acknowledge pulse
	iecSendWaitRFD2   // wait for DATA to come back up after CLK went low
	iecSendBitSetup   // T_S: bit on DATA, CLK still low
	iecSendBitValid   // T_V: CLK released, bit is valid
	iecSendWaitFrame  // step 4: wait for the listener's frame handshake
	iecSendBetween    // T_BB: minimum gap between bytes
	iecTalkDone       // nothing left to send, still holding CLK as talker
)

// Serial bus timings, in Phi2 cycles. One PAL cycle is 1.015us, close
// enough to the microsecond that the table is quoted in that no scaling
// is worth the confusion.
const (
	// iecEOIThreshold is how long a listener waits at "ready for data"
	// before deciding the talker is signalling EOI. The table says 200us
	// (T_YE); the 1541 realises it as VIA1 timer 1 loaded with $01xx,
	// which is 256 cycles, so that is what we use.
	iecEOIThreshold = 256

	// iecEOIAckHold is how long the listener holds DATA low to say "EOI
	// noted" (T_EI). The table's minimum is 60us, but note 5 raises it to
	// 80us when the listener is a peripheral, which we are.
	iecEOIAckHold = 80

	// iecBitSetup and iecBitValid are the two halves of a transmitted bit:
	// the bit sits on DATA with CLK still asserted (T_S), then CLK is
	// released to mark it valid (T_V). The published minima are 20us and
	// 20us, the latter raised to 60us by note 4 of the timing table when a
	// peripheral is the talker.
	//
	// Neither minimum is usable here, because the C64 does not detect
	// edges. ACPTR polls the port in level-triggered loops: one waiting
	// for the talker's first CLK assertion ($EE30, about 40 cycles a
	// pass), one waiting for CLK released ($EE5A) and one waiting for CLK
	// asserted again ($EE67), the last two about 15 cycles a pass. Every
	// read is debounced by a second read that costs another pass whenever
	// it straddles a transition, and the VIC-II can stall the CPU for 43
	// cycles on a badline. The worst-case gap between two consecutive
	// samples is therefore around 75 cycles, and a phase shorter than that
	// can fall between them and go unseen.
	//
	// Missing a phase does not merely slow the transfer down, it corrupts
	// the byte. If ACPTR misses a CLK assertion it stays in the $EE67
	// loop, is satisfied by the *next* bit's assertion instead, and so
	// drops a bit and then hangs waiting for a ninth that never comes.
	// Both phases are held well clear of the sampling gap. The resulting
	// rate is about 570 bytes a second, still quicker than a real 1541.
	iecBitSetup = 110
	iecBitValid = 110

	// iecTurnaroundDelay is T_TK, the pause between the C64 releasing ATN
	// and the device taking over the CLOCK line (20-100us).
	iecTurnaroundDelay = 30

	// iecTurnaroundHold is T_DA, how long the device holds CLK low after
	// the turnaround before it may start a byte, so the C64's "wait for
	// the talker to grab CLK" loop is guaranteed to see it.
	iecTurnaroundHold = 100

	// iecBetweenBytes is T_BB, the minimum gap between the frame
	// handshake of one byte and the "ready to send" of the next.
	iecBetweenBytes = 100
)

// Command bytes sent under ATN.
const (
	iecListen   = 0x20 // + primary address
	iecUnlisten = 0x3F
	iecTalk     = 0x40 // + primary address
	iecUntalk   = 0x5F
	iecSecond   = 0x60 // + channel: select channel for data
	iecClose    = 0xE0 // + channel
	iecOpen     = 0xF0 // + channel
)

// virtualDrive is the singleton generic drive, if one is attached.
var virtualDrive iecDevice

// virtualDriveAttached reports whether AttachVirtualDrive/AttachVirtualPRG
// has put the generic drive on the bus and it hasn't since been detached.
var virtualDriveAttached bool

// AttachVirtualDrive puts a generic CBM-DOS drive on the IEC bus at the
// given primary address (8 for the first drive), replacing any virtual
// drive already there. It speaks the serial protocol on the wire but
// implements the DOS in Go against a D64 image, with no drive CPU and no
// GCR to model.
func AttachVirtualDrive(address uint8) {
	virtualDrive = iecDevice{address: address}
	virtualDrive.dos.reset()
	attachIEC(&virtualDrive)
	virtualDriveAttached = true
}

// AttachVirtualPRG puts a read-only generic drive on the IEC bus at address
// serving name as a PRG. It avoids the 175KB D64 allocation needed by
// AttachVirtualDrive, making it suitable for TinyGo targets with tight RAM.
//
// data must include the PRG's two-byte load address and must not be modified
// while the drive is attached.
func AttachVirtualPRG(address uint8, name string, data []byte) {
	virtualDrive = iecDevice{address: address}
	virtualDrive.dos.reset()
	virtualDrive.dos.files = []virtualFile{{
		name: name,
		typ:  ftypePRG,
		data: data,
	}}
	attachIEC(&virtualDrive)
	virtualDriveAttached = true
}

// DetachVirtualDrive removes the generic drive from the IEC bus.
func DetachVirtualDrive() {
	detachIEC(&virtualDrive)
	virtualDriveAttached = false
}

func (d *iecDevice) iecCLKOut() bool  { return d.clk }
func (d *iecDevice) iecDATAOut() bool { return d.data }

// iecAddress is the address this drive answers to. Unlike the 1541, whose
// address is set by jumpers on the board, this one is whatever it was
// attached as.
func (d *iecDevice) iecAddress() uint8 { return d.address }

// clkIn and dataIn sample the bus. The device never samples a line it is
// driving itself - the protocol is arranged so that whoever is listening
// on a line has released it - so there is no need to subtract our own
// contribution here.
func (d *iecDevice) clkIn() bool  { return CLKAsserted() }
func (d *iecDevice) dataIn() bool { return DATAAsserted() }

func (d *iecDevice) setState(s iecState) {
	d.state = s
	d.timer = 0
}

// iecTick advances the device by one Phi2 cycle.
//
// ATN is handled before anything else, because it overrides everything:
// "when ATN is pulled true, everybody stops what they are doing". A real
// 1541 does this in two halves - the ATN/ATNA gate pulls DATA low in
// hardware the instant ATN falls, guaranteeing the 1000us T_AT response
// even if the drive CPU is busy, and the DOS then resets its stack and
// restarts the bus service routine. We do both at once.
func (d *iecDevice) iecTick() {
	switch atn := ATNAsserted(); {
	case atn && !d.atn:
		d.beginATN()
		return
	case !atn && d.atn:
		d.endATN()
		return
	}

	d.timer++

	switch d.state {
	case iecIdle:
		// Either unaddressed, or addressed to another device and waiting
		// for ATN to go away. Nothing to do until a line moves, so stop
		// the bus clocking us until the C64 moves one. Only when this is
		// the whole bus: a second device may still have work in hand.
		if len(iecBus) == 1 {
			iecActive = false
		}

	case iecAtnWaitCLK:
		// When the controller asserts ATN it also "pulls CLK and
		// releases DATA, because it is now the sender" - but not in the
		// same instant, and CLK is usually still released from the last
		// command when ATN falls. Waiting for the controller to take CLK
		// before looking for it to let go again is what keeps us from
		// mistaking the tail of the previous transfer for a "ready to
		// send". A real 1541 gets this for free: it has to take an IRQ
		// and reset its stack first, which takes longer than the C64
		// needs to reach its own "assert CLK".
		if d.clkIn() {
			d.setState(iecRecvReady)
		}

	// ----- receiving, as listener -----

	case iecRecvReady:
		// Step 1: the talker releases CLK when it has a byte to send.
		// There is no time limit on us noticing (T_H is "infinite").
		if !d.clkIn() {
			d.releaseDATA() // step 2: "ready for data"
			d.bit, d.shift, d.eoi = 0, 0, false
			d.setState(iecRecvWaitCLK)
		}

	case iecRecvWaitCLK:
		switch {
		case d.clkIn():
			// The talker pulled CLK back down inside the window, so this
			// is an ordinary byte: go straight to the bits.
			d.setState(iecRecvBitValid)
		case d.timer >= iecEOIThreshold:
			// T_YE elapsed with CLK still released: the talker is
			// signalling that this is the last byte. Acknowledge by
			// pulling DATA for T_EI and then letting go.
			d.eoi = true
			d.assertDATA()
			d.setState(iecRecvEOIHold)
		}

	case iecRecvEOIHold:
		if d.timer >= iecEOIAckHold {
			d.releaseDATA()
			d.setState(iecRecvEOIWait)
		}

	case iecRecvEOIWait:
		if d.clkIn() {
			d.setState(iecRecvBitValid)
		}

	case iecRecvBitValid:
		// Step 3: the bit on DATA is valid the moment CLK is released.
		// DATA released is a one bit, DATA pulled low is a zero, and the
		// bits arrive least significant first.
		if !d.clkIn() {
			d.shift >>= 1
			if !d.dataIn() {
				d.shift |= 0x80
			}
			d.setState(iecRecvBitDone)
		}

	case iecRecvBitDone:
		if d.clkIn() {
			d.bit++
			if d.bit < 8 {
				d.setState(iecRecvBitValid)
				break
			}
			// Step 4: the frame handshake. Pull DATA to say the byte
			// arrived; the talker gives up after T_F (1000us) if we
			// don't. Then hold it - we are back at step zero, listener
			// holding DATA - and take the byte.
			d.assertDATA()
			d.setState(iecRecvReady)
			d.receiveByte(d.shift, d.eoi)
		}

	// ----- turnaround and sending, as talker -----

	case iecTurnaround:
		// T_TK after ATN went away: hand DATA back to the C64 (which is
		// already holding it as the new listener) and take CLK.
		if d.timer >= iecTurnaroundDelay {
			d.releaseDATA()
			d.assertCLK()
			d.setState(iecSendHold)
		}

	case iecSendHold:
		// The C64's TKSA spins until it sees CLK pulled low. Hold it for
		// T_DA before touching it again so that loop cannot miss it.
		if d.timer >= iecTurnaroundHold {
			d.setState(iecSendReady)
		}

	case iecSendReady:
		b, eoi, ok := d.dos.talkByte()
		if !ok {
			// Nothing to send at all - a file that wasn't found. There
			// is no error code on the wire for that, so do what a 1541
			// does: let go of both lines and let the C64's ACPTR time
			// out, which sets ST and produces FILE NOT FOUND.
			d.releaseCLK()
			d.releaseDATA()
			d.setState(iecTalkDone)
			break
		}
		d.shift, d.eoi, d.bit = b, eoi, 0
		d.releaseCLK() // step 1: "ready to send"
		d.setState(iecSendWaitRFD)

	case iecSendWaitRFD:
		// Step 2: wait for the listener to release DATA. It may take as
		// long as it likes.
		if !d.dataIn() {
			if d.eoi {
				// Signal EOI by simply not pulling CLK: after 200us the
				// listener works it out and answers with a DATA pulse.
				d.setState(iecSendWaitEOIAck)
				break
			}
			d.assertCLK()
			d.setState(iecSendWaitRFD2)
		}

	case iecSendWaitEOIAck:
		if d.dataIn() {
			d.assertCLK()
			d.setState(iecSendWaitRFD2)
		}

	case iecSendWaitRFD2:
		// After pulling CLK we need DATA released before clocking bits
		// out; for a non-EOI byte it already is, for an EOI byte we are
		// waiting out the tail of the listener's acknowledge pulse.
		if !d.dataIn() {
			d.setState(iecSendBitSetup)
		}

	case iecSendBitSetup:
		// Step 3: put the bit on DATA (one = released) with CLK still
		// low, and let it settle for T_S.
		if d.timer == 1 {
			if d.shift&1 != 0 {
				d.releaseDATA()
			} else {
				d.assertDATA()
			}
			d.shift >>= 1
		}
		if d.timer >= iecBitSetup {
			d.releaseCLK() // the bit is now valid
			d.setState(iecSendBitValid)
		}

	case iecSendBitValid:
		if d.timer >= iecBitValid {
			d.assertCLK()
			d.releaseDATA()
			d.bit++
			if d.bit < 8 {
				d.setState(iecSendBitSetup)
			} else {
				d.setState(iecSendWaitFrame)
			}
		}

	case iecSendWaitFrame:
		// Step 4: the listener pulls DATA to acknowledge the byte.
		if d.dataIn() {
			if d.eoi {
				d.setState(iecTalkDone)
			} else {
				d.setState(iecSendBetween)
			}
		}

	case iecSendBetween:
		if d.timer >= iecBetweenBytes {
			d.setState(iecSendReady)
		}

	case iecTalkDone:
		// Still nominally the talker, holding CLK, with nothing more to
		// say. The C64 will UNTALK us.
	}
}

// beginATN handles the ATN falling edge: abandon whatever transfer was in
// progress, acknowledge by pulling DATA within T_AT, release CLK because
// we are a listener now, and start taking command bytes.
func (d *iecDevice) beginATN() {
	d.atn = true
	d.listening = false
	d.talking = false
	d.releaseCLK()
	d.assertDATA()
	d.setState(iecAtnWaitCLK)
}

// endATN handles ATN being released, which is what actually starts the
// data phase: a device that was told to LISTEN stays a listener and keeps
// holding DATA, one that was told to TALK turns the bus around, and one
// that was addressed to neither drops off the bus entirely.
func (d *iecDevice) endATN() {
	d.atn = false
	switch {
	case d.listening:
		d.dos.beginListen(d.secondary)
		d.assertDATA()
		d.releaseCLK()
		d.setState(iecRecvReady)
	case d.talking:
		d.dos.beginTalk(d.secondary)
		d.setState(iecTurnaround)
	default:
		d.releaseCLK()
		d.releaseDATA()
		d.setState(iecIdle)
	}
}

// receiveByte disposes of a byte that has just been clocked in: a command
// under ATN, otherwise data for whichever channel is open.
func (d *iecDevice) receiveByte(b uint8, eoi bool) {
	if !d.atn {
		d.dos.listenByte(b, eoi)
		return
	}

	switch {
	case b == iecUnlisten:
		// UNLISTEN terminates a filename or a command string, so the DOS
		// acts on it rather than merely forgetting it. Note this is not
		// gated on d.listening: ATN service has already cleared that
		// flag by the time UNLISTEN itself arrives, exactly as it does
		// on a 1541, so the DOS is what remembers there is something
		// outstanding.
		d.dos.unlisten()
		d.listening = false
	case b == iecUntalk:
		d.dos.untalk()
		d.talking = false
	case b == iecTalk+d.address:
		d.talking, d.listening = true, false
	case b == iecListen+d.address:
		d.listening, d.talking = true, false
	case b&0x60 == 0x60:
		// A secondary address: $6x selects a channel, $Ex closes it, $Fx
		// opens it. Bit 4 is ignored, so $6F and $7F are both channel 15.
		// Note this is accepted whether or not we are the addressed
		// device - the LISTEN/TALK flags are what gate the data phase.
		d.secondary = b
		if b&0xF0 == iecClose {
			d.dos.close(b & 0x0F)
		}
	default:
		// A LISTEN or TALK for somebody else. We still acknowledged the
		// byte - every device does, which is why "OPEN 6,6" reports no
		// error - but now we drop off the bus until ATN is released.
		d.releaseCLK()
		d.releaseDATA()
		d.setState(iecIdle)
	}
}

func (d *iecDevice) assertCLK()   { d.clk = true }
func (d *iecDevice) releaseCLK()  { d.clk = false }
func (d *iecDevice) assertDATA()  { d.data = true }
func (d *iecDevice) releaseDATA() { d.data = false }

// stateName describes the device's protocol state, for IECStatus.
func (d *iecDevice) stateName() string {
	switch d.state {
	case iecIdle:
		return "idle"
	case iecAtnWaitCLK, iecRecvReady, iecRecvWaitCLK, iecRecvEOIHold,
		iecRecvEOIWait, iecRecvBitValid, iecRecvBitDone:
		return "listening"
	case iecTurnaround, iecSendHold:
		return "turnaround"
	case iecTalkDone:
		return "talk-idle"
	default:
		return "talking"
	}
}

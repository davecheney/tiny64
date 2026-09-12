package tiny64

import "strings"

// cbmDOS is the file system side of the generic drive: everything above
// the wire. It sees the stream of secondary addresses and bytes that
// iecDevice pulls off the bus, and turns them into opens, closes, reads,
// writes and commands against the D64 image in the drive.
//
// The channel model is CBM DOS's: sixteen channels, selected by the low
// nibble of a secondary address. Channel 15 is the command/status
// channel; the rest carry file data. By convention the KERNAL uses
// channel 0 for LOAD and channel 1 for SAVE, which is why those two work
// without a file type being named.
type cbmDOS struct {
	ch [16]cbmChannel

	// files, when non-nil, is a compact read-only file set instead of the
	// D64-backed filesystem. It lets a TinyGo target expose a small demo
	// program without consuming RAM for an entire disk image.
	files []virtualFile

	// The secondary address currently being serviced, split into what it
	// asked for ($6x data, $Ex close, $Fx open) and which channel.
	mode    uint8
	channel uint8

	// buf accumulates a filename (during OPEN) or a command string (on
	// channel 15), both of which are terminated by UNLISTEN rather than
	// by EOI.
	buf []byte

	// pending is set while buf is collecting something that has to be
	// acted on once the C64 says it is finished - a filename or a
	// command string. Which of the two, and for which channel, is in
	// mode and channel.
	pending bool

	// talk is the channel currently being read from.
	talk uint8

	// The error channel: code, message and the track/sector the last
	// operation touched, formatted as "NN, MESSAGE,TT,SS" on read.
	errCode           int
	errTrack, errSect int
}

// cbmChannel is one open channel. A channel is either reading (read holds
// the whole file, already fetched) or writing (write accumulates until
// CLOSE commits it), never both.
type cbmChannel struct {
	open    bool
	writing bool

	read []byte
	pos  int

	write []byte
	name  string
	typ   uint8
}

type virtualFile struct {
	name string
	typ  uint8
	data []byte
}

// reset returns the DOS to power-on state: no channels open, status
// "73, CBM DOS V2.6 1541", which is what a drive reports until something
// clears it.
func (d *cbmDOS) reset() {
	*d = cbmDOS{errCode: 73}
}

// beginListen is called when ATN is released and this device is the
// listener: the bytes that follow belong to the secondary address sa.
func (d *cbmDOS) beginListen(sa uint8) {
	d.mode, d.channel = sa&0xF0, sa&0x0F
	if d.mode == iecOpen || (d.mode == iecSecond && d.channel == 15) {
		d.buf = d.buf[:0]
		d.pending = true
	}
}

// listenByte takes one byte of the data phase.
//
// A filename or a command string ends in one of two ways. The protocol
// says UNLISTEN terminates it, and a device has to honour that; but the
// C64's KERNAL also holds the last byte back and sends it with EOI (which
// is why CIOUT always transmits one byte behind), so in practice EOI
// arrives first. Either will do here.
func (d *cbmDOS) listenByte(b uint8, eoi bool) {
	switch {
	case d.pending:
		d.buf = append(d.buf, b)
		if eoi {
			d.finish()
		}
	case d.mode == iecSecond:
		c := &d.ch[d.channel]
		if c.open && c.writing {
			c.write = append(c.write, b)
		}
	}
}

// unlisten is called when the C64 sends UNLISTEN. By then ATN service has
// already forgotten that this device was the listener, so whether there
// is anything outstanding is the DOS's business, not the bus layer's.
func (d *cbmDOS) unlisten() {
	d.finish()
}

// finish acts on a completed filename or command string. It is idempotent
// because both EOI and UNLISTEN reach it, usually both for the same
// string.
func (d *cbmDOS) finish() {
	if !d.pending {
		return
	}
	d.pending = false
	switch {
	case d.mode == iecOpen && d.channel == 15:
		d.command(string(d.buf))
	case d.mode == iecOpen:
		d.open(d.channel, string(d.buf))
	case d.channel == 15:
		if len(d.buf) > 0 {
			d.command(string(d.buf))
		}
	}
	d.buf = d.buf[:0]
}

// beginTalk is called when ATN is released and this device is the talker:
// point the read stream at the channel the secondary address selected.
func (d *cbmDOS) beginTalk(sa uint8) {
	d.talk = sa & 0x0F
	if d.talk == 15 {
		// Reading the command channel always returns the current status
		// from the start, and clears it back to "00, OK".
		c := &d.ch[15]
		c.read = []byte(d.status())
		c.pos = 0
		c.open = true
		d.setError(0, 0, 0)
	}
}

// untalk ends the read. A file channel keeps its position, so a partial
// read can be resumed by addressing it again.
func (d *cbmDOS) untalk() {}

// talkByte returns the next byte of the read stream, whether it is the
// last (which the device signals as EOI), and whether there was anything
// to send at all. A false ok is how "file not found" reaches the C64:
// there is no error code on the serial bus, so the drive simply lets go
// of the lines and lets the KERNAL time out.
func (d *cbmDOS) talkByte() (b uint8, eoi bool, ok bool) {
	c := &d.ch[d.talk]
	if !c.open || c.pos >= len(c.read) {
		return 0, false, false
	}
	b = c.read[c.pos]
	c.pos++
	return b, c.pos >= len(c.read), true
}

// open handles OPEN on a data channel. The name may carry a type and a
// direction after commas ("DATA,S,W"), and a leading "$" asks for the
// directory instead of a file. Channels 0 and 1 default to reading and
// writing a PRG, which is what LOAD and SAVE rely on.
func (d *cbmDOS) open(channel uint8, name string) {
	c := &d.ch[channel]
	*c = cbmChannel{}

	name = strings.TrimPrefix(name, "0:")
	fields := strings.Split(name, ",")
	base := fields[0]

	typ := uint8(ftypeDEL) // "whatever type it happens to be"
	write := channel == 1
	for _, f := range fields[1:] {
		if f == "" {
			continue
		}
		switch f[0] {
		case 'P':
			typ = ftypePRG
		case 'S':
			typ = ftypeSEQ
		case 'U':
			typ = ftypeUSR
		case 'L':
			typ = ftypeREL
		case 'R':
			write = false
		case 'W':
			write = true
		case 'A':
			write = true
		}
	}

	if strings.HasPrefix(base, "$") {
		c.open = true
		c.read = d.directory(strings.TrimPrefix(base, "$"))
		d.setError(0, 0, 0)
		return
	}
	if base == "" {
		d.setError(34, 0, 0) // SYNTAX ERROR (no file given)
		return
	}

	if d.files != nil {
		if write {
			d.setError(26, 0, 0) // WRITE PROTECT ON
			return
		}
		for _, file := range d.files {
			if (typ == ftypeDEL || typ == file.typ) && cbmMatch(base, file.name) {
				c.open = true
				c.read = file.data
				d.setError(0, 0, 0)
				return
			}
		}
		d.setError(62, 0, 0) // FILE NOT FOUND
		return
	}

	if write {
		// Save-with-replace: "@:NAME" overwrites silently, anything else
		// refuses to clobber an existing file.
		replace := strings.HasPrefix(base, "@")
		base = strings.TrimPrefix(base, "@")
		base = strings.TrimPrefix(base, "0:")
		base = strings.TrimPrefix(base, ":")
		if _, found := diskFind(base, ftypeDEL); found {
			if !replace {
				d.setError(63, 0, 0) // FILE EXISTS
				return
			}
		}
		if typ == ftypeDEL {
			typ = ftypePRG
		}
		c.open, c.writing, c.name, c.typ = true, true, base, typ
		d.setError(0, 0, 0)
		return
	}

	entry, found := diskFind(base, typ)
	if !found {
		d.setError(62, 0, 0) // FILE NOT FOUND
		return
	}
	c.open = true
	c.read = diskReadFile(entry)
	d.setError(0, 0, 0)
}

// close closes a channel, committing anything written to it. Closing
// channel 15 closes every channel, as it does on a real drive.
func (d *cbmDOS) close(channel uint8) {
	if channel == 15 {
		for i := range d.ch {
			d.closeOne(uint8(i))
		}
		return
	}
	d.closeOne(channel)
}

func (d *cbmDOS) closeOne(channel uint8) {
	c := &d.ch[channel]
	if c.open && c.writing {
		if code := diskWriteFile(c.name, c.typ, c.write); code != 0 {
			d.setError(code, 0, 0)
		} else {
			d.setError(0, 0, 0)
		}
	}
	*c = cbmChannel{}
}

// command executes a DOS command sent to channel 15. Only the commands
// that a drive without a disk controller can meaningfully implement are
// here; the rest report an unknown-command error rather than pretending.
func (d *cbmDOS) command(cmd string) {
	cmd = strings.TrimRight(cmd, "\r\n")
	if cmd == "" {
		return
	}
	arg := ""
	if i := strings.IndexAny(cmd, ":"); i >= 0 {
		arg = cmd[i+1:]
	}

	switch cmd[0] {
	case 'I': // INITIALIZE: re-read the BAM. Nothing is cached, so this
		// only has to clear the error channel.
		d.setError(0, 0, 0)
	case 'V': // VALIDATE: rebuild the BAM from the directory.
		d.setError(0, 0, 0)
	case 'S': // SCRATCH
		if n := diskScratch(arg); n > 0 {
			d.setError(1, n, 0) // "01, FILES SCRATCHED,nn,00"
		} else {
			d.setError(62, 0, 0)
		}
	case 'N': // NEW: format
		name, id := arg, "01"
		if i := strings.Index(arg, ","); i >= 0 {
			name, id = arg[:i], arg[i+1:]
		}
		replaceDisk(FormatDisk(name, id))
		d.setError(0, 0, 0)
	case 'U': // U: / UJ - reset the drive
		d.reset()
	default:
		d.setError(31, 0, 0) // SYNTAX ERROR (unknown command)
	}
}

func (d *cbmDOS) setError(code, track, sector int) {
	d.errCode, d.errTrack, d.errSect = code, track, sector
}

// status formats the error channel the way a 1541 does: a two-digit code,
// the message, the track and sector involved, and a trailing carriage
// return.
func (d *cbmDOS) status() string {
	msg := "OK"
	switch d.errCode {
	case 0:
		msg = "OK"
	case 1:
		msg = "FILES SCRATCHED"
	case 26:
		msg = "WRITE PROTECT ON"
	case 31:
		msg = "SYNTAX ERROR"
	case 34:
		msg = "SYNTAX ERROR"
	case 62:
		msg = "FILE NOT FOUND"
	case 63:
		msg = "FILE EXISTS"
	case 72:
		msg = "DISK FULL"
	case 73:
		msg = "CBM DOS V2.6 1541"
	case 74:
		msg = "DRIVE NOT READY"
	}
	return twoDigit(d.errCode) + "," + msg + "," +
		twoDigit(d.errTrack) + "," + twoDigit(d.errSect) + "\r"
}

func twoDigit(n int) string {
	if n < 0 || n > 99 {
		n = 0
	}
	return string([]byte{byte('0' + n/10), byte('0' + n%10)})
}

// directory builds the "$" listing: a BASIC program the C64 can LOAD and
// LIST, with each line's line number standing in for the block count.
// pattern, if given, filters the entries the way LOAD"$:*=P" does.
func (d *cbmDOS) directory(pattern string) []byte {
	pattern = strings.TrimPrefix(pattern, "0")
	pattern = strings.TrimPrefix(pattern, ":")

	out := []byte{0x01, 0x04} // load address $0401, where BASIC lives

	// Header line: the disk name in reverse video, then the ID and DOS
	// type, all inside quotes so LIST renders it like the real thing.
	// The five bytes at $A2 are copied out verbatim rather than read as
	// an ID, a separator and a DOS version. That is what the drive does,
	// and on a freshly formatted disk the two are indistinguishable
	// because $A4 is left at $A0. Real disks do not all keep to that:
	// some use the whole field as one five-character string, so parsing
	// it eats the middle character.
	//
	// $A0 is the shifted space CBM DOS pads with, and it has to become a
	// real space here for the same reason the filename padding does: this
	// text is handed to LIST as a BASIC line, and $A0 there is the token
	// for CLOSE.
	tail := "   2A"
	if bam := diskReadSector(dirTrack, 0); bam != nil {
		t := append([]byte(nil), bam[0xA2:0xA7]...)
		for i, c := range t {
			if c == 0xA0 {
				t[i] = ' '
			}
		}
		tail = string(t)
	}
	header := append([]byte{0x12, '"'}, padTo(diskName(), 16)...)
	header = append(header, '"', ' ')
	header = append(header, tail...)
	out = dirLine(out, 0, header)

	// Every occupied slot is listed, including DEL entries. A scratched
	// file has its type byte zeroed, which diskDirectory already treats
	// as a free slot, so there is nothing left here to filter: a slot
	// that survives to this point is one the drive would show.
	for _, e := range diskDirectory() {
		if pattern != "" && !cbmMatch(pattern, e.nameString()) {
			continue
		}
		var text []byte
		// The block count is the line number, so the entry text is
		// indented to keep the filenames in a column whatever its width.
		for pad := len(itoa(int(e.blocks))); pad < 4; pad++ {
			text = append(text, ' ')
		}
		text = append(text, '"')
		text = append(text, e.nameString()...)
		text = append(text, '"')
		// The name field is sixteen columns wide. The seventeenth is the
		// flag appended below, which is a space for a properly closed
		// file and an asterisk for one the drive never closed.
		for pad := len(e.nameString()); pad < 16; pad++ {
			text = append(text, ' ')
		}
		if !e.closed() {
			text = append(text, '*')
		} else {
			text = append(text, ' ')
		}
		text = append(text, ftypeName(e.fileType())...)
		// A locked file is shown with a trailing '<', the counterpart to
		// the '*' that marks an unclosed one.
		if e.typ&ftypeLocked != 0 {
			text = append(text, '<')
		}
		out = dirLine(out, e.blocks, text)
	}

	free := 0
	if bam := diskReadSector(dirTrack, 0); bam != nil {
		free = bamBlocksFree(bam)
	}
	out = dirLine(out, uint16(free), []byte("BLOCKS FREE."))
	return append(out, 0x00, 0x00) // end of program
}

// dirLine appends one BASIC line: a link to the next line (any non-zero
// address will do, since the C64 relinks after LOAD), the line number,
// the text, and a terminating zero.
func dirLine(out []byte, number uint16, text []byte) []byte {
	out = append(out, 0x01, 0x01)
	out = append(out, uint8(number), uint8(number>>8))
	out = append(out, text...)
	return append(out, 0x00)
}

func padTo(s string, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		if i < len(s) {
			out[i] = s[i]
		} else {
			out[i] = ' '
		}
	}
	return out
}

func ftypeName(t uint8) string {
	switch t {
	case ftypeSEQ:
		return "SEQ"
	case ftypePRG:
		return "PRG"
	case ftypeUSR:
		return "USR"
	case ftypeREL:
		return "REL"
	default:
		return "DEL"
	}
}

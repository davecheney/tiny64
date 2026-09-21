//go:build tufty2040

package main

import (
	"errors"
	"time"
	_ "unsafe"

	"machine"

	"github.com/davecheney/tiny64"
	pio "github.com/tinygo-org/pio/rp2-pio"
	"github.com/tinygo-org/pio/rp2-pio/piolib"
)

var errAsyncFramePending = errors.New("async frame transfer already pending")

type parallelST7789 struct {
	cs machine.Pin
	dc machine.Pin
	rd machine.Pin
	bl machine.Pin

	pl *piolib.Parallel

	asyncFramePending bool
	buf               [4]byte
}

func (st *parallelST7789) setBacklight(on bool) error {
	if st.bl == machine.NoPin {
		return nil
	}
	pwm := machine.PWM1
	pwm.Configure(machine.PWMConfig{})
	ch, err := pwm.Channel(st.bl)
	if err != nil {
		return err
	}
	if on {
		pwm.Set(ch, pwm.Top())
		return nil
	}
	pwm.Set(ch, 0)
	return nil
}

func (st *parallelST7789) init() error {
	time.Sleep(10 * time.Millisecond)
	if err := st.setBacklight(false); err != nil {
		return err
	}

	if err := st.command(swreset, nil); err != nil {
		return err
	}
	time.Sleep(150 * time.Millisecond)

	initCommands := []struct {
		command byte
		data    []byte
	}{
		{colmod, []byte{0x05}},
		{porctrl, []byte{0x0c, 0x0c, 0x00, 0x33, 0x33}},
		{lcmctrl, []byte{0x2c}},
		{vdvvrhen, []byte{0x01}},
		{vrhs, []byte{0x12}},
		{vdvs, []byte{0x20}},
		{pwctrl1, []byte{0xa4, 0xa1}},
		{frctrl2, []byte{0x0f}},
		{ramctrl, []byte{0x00, 0xc0}},
		{gctrl, []byte{0x35}},
		{vcoms, []byte{0x1b}},
		{gmctrp1, []byte{0xf0, 0x00, 0x06, 0x04, 0x05, 0x05, 0x31, 0x44, 0x48, 0x36, 0x12, 0x12, 0x2b, 0x34}},
		{gmctrn1, []byte{0xf0, 0x0b, 0x0f, 0x0f, 0x0d, 0x26, 0x31, 0x43, 0x47, 0x38, 0x14, 0x14, 0x2c, 0x32}},
		{invon, nil},
		{slpout, nil},
	}
	for _, cmd := range initCommands {
		if err := st.command(cmd.command, cmd.data); err != nil {
			return err
		}
	}

	time.Sleep(100 * time.Millisecond)
	if err := st.configureDisplay(); err != nil {
		return err
	}
	if err := st.command(teon, []byte{0x00}); err != nil {
		return err
	}
	if err := st.command(ste, []byte{0x00, 0x00}); err != nil {
		return err
	}
	if err := st.command(dispon, nil); err != nil {
		return err
	}

	time.Sleep(50 * time.Millisecond)
	return st.setBacklight(true)
}

func (st *parallelST7789) configureDisplay() error {
	madctl := byte(rowOrder | swapXY | scanOrder)
	if err := st.setWindow(0, 0, displayWidth, displayHeight); err != nil {
		return err
	}
	return st.command(madctlCommand, []byte{madctl})
}

//go:section .ramfuncs
func (st *parallelST7789) start(frame []byte) error {
	if st.asyncFramePending {
		return errAsyncFramePending
	}
	if err := st.setWindow(0, 0, displayWidth, displayHeight); err != nil {
		return err
	}

	st.dc.Low()
	st.cs.Low()
	if err := st.pl.Tx8([]byte{ramwr}); err != nil {
		st.cs.High()
		return err
	}
	time.Sleep(10 * time.Microsecond)
	st.dc.High()
	if err := st.pl.Tx8Async(frame); err != nil {
		st.cs.High()
		return err
	}
	st.asyncFramePending = true
	return nil
}

//go:section .ramfuncs
func (st *parallelST7789) wait() {
	if !st.asyncFramePending {
		return
	}
	st.pl.WaitTxAsync()
	time.Sleep(10 * time.Microsecond)
	st.cs.High()
	st.asyncFramePending = false
}

//go:section .ramfuncs
func (st *parallelST7789) setWindow(x, y, w, h int16) error {
	st.buf[0] = byte(x >> 8)
	st.buf[1] = byte(x)
	st.buf[2] = byte((x + w - 1) >> 8)
	st.buf[3] = byte(x + w - 1)
	if err := st.command(caset, st.buf[:]); err != nil {
		return err
	}
	st.buf[0] = byte(y >> 8)
	st.buf[1] = byte(y)
	st.buf[2] = byte((y + h - 1) >> 8)
	st.buf[3] = byte(y + h - 1)
	return st.command(raset, st.buf[:])
}

//go:section .ramfuncs
func (st *parallelST7789) command(command byte, data []byte) error {
	st.dc.Low()
	st.cs.Low()
	if err := st.pl.Tx8([]byte{command}); err != nil {
		st.cs.High()
		return err
	}
	if len(data) > 0 {
		time.Sleep(10 * time.Microsecond)
		st.dc.High()
		if err := st.pl.Tx8(data); err != nil {
			st.cs.High()
			return err
		}
	}
	time.Sleep(10 * time.Microsecond)
	st.cs.High()
	return nil
}

// busBaud is the parallel bus clock. The panel is rated higher, but the
// frame has to be out before the next one is emulated and this already
// clears that with room.
const busBaud = 15_000_000

// configurePanel brings up the Tufty's eight bit parallel bus through PIO
// and DMA, which is what makes start asynchronous: the transfer runs in
// hardware while the next frame is emulated.
func configurePanel() (*parallelST7789, error) {
	machine.LCD_CS.Configure(machine.PinConfig{Mode: machine.PinOutput})
	machine.LCD_CS.High()
	machine.LCD_DC.Configure(machine.PinConfig{Mode: machine.PinOutput})
	machine.LCD_DC.High()
	machine.LCD_RD.Configure(machine.PinConfig{Mode: machine.PinOutput})
	machine.LCD_RD.High()

	sm, err := pio.PIO0.ClaimStateMachine()
	if err != nil {
		return nil, err
	}
	bus, err := piolib.NewParallel(sm, piolib.ParallelConfig{
		Baud:        busBaud,
		Clock:       machine.LCD_WR,
		DataBase:    machine.LCD_DB0,
		BusWidth:    8,
		BitsPerPull: 8,
	})
	if err != nil {
		return nil, err
	}
	if err := bus.EnableDMA(true); err != nil {
		return nil, err
	}

	display := &parallelST7789{
		cs: machine.LCD_CS,
		dc: machine.LCD_DC,
		rd: machine.LCD_RD,
		bl: machine.LCD_BACKLIGHT,
		pl: bus,
	}
	if err := display.init(); err != nil {
		return nil, err
	}
	return display, nil
}

// buttonState tracks the three buttons this board gives the machine, so
// each one acts on its press edge rather than for as long as it is held.
type buttonState struct {
	a, b, c bool
}

// configureButtons readies the board's buttons. The Tufty has A, B and C
// along the bottom edge; the Gopher Badge has no C, which is why this
// lives beside the panel rather than in main.go.
func configureButtons() (*buttonState, error) {
	for _, pin := range []machine.Pin{
		machine.BUTTON_A,
		machine.BUTTON_B,
		machine.BUTTON_C,
		machine.BUTTON_UP,
		machine.BUTTON_DOWN,
	} {
		pin.Configure(machine.PinConfig{Mode: machine.PinInputPulldown})
	}
	return &buttonState{}, nil
}

func (s *buttonState) poll() {
	a := machine.BUTTON_A.Get()
	if a && !s.a {
		// Button A edge: cold reset, and start the demo again.
		randomiseMemory()
		tiny64.Keys().ReleaseAll()
		tiny64.Reset()
		tiny64.Autostart()
	}
	s.a = a

	b := machine.BUTTON_B.Get()
	if b && !s.b {
		// Button B edge: RUN/STOP + RESTORE, the C64's warm reset.
		tiny64.Keys().Press(tiny64.KeyRunStop)
		tiny64.Keys().Restore()
	} else if !b && s.b {
		tiny64.Keys().Release(tiny64.KeyRunStop)
	}
	s.b = b

	c := machine.BUTTON_C.Get()
	if c && !s.c {
		// Button C edge: RESTORE alone, which is an NMI pulse.
		tiny64.Keys().Restore()
	}
	s.c = c
}

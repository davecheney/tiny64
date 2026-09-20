//go:build tinygo

package main

import (
	"errors"
	"time"
	_ "unsafe"

	"machine"

	"github.com/tinygo-org/pio/rp2-pio/piolib"
)

const (
	displayWidth  = 320
	displayHeight = 240
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
func (st *parallelST7789) startDisplay(frame []byte) error {
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
func (st *parallelST7789) waitDisplay() {
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

//go:build tufty2040

package main

const (
	swreset       = 0x01
	teon          = 0x35
	ste           = 0x44
	madctlCommand = 0x36
	colmod        = 0x3a
	ramctrl       = 0xb0
	gctrl         = 0xb7
	vcoms         = 0xbb
	lcmctrl       = 0xc0
	vdvvrhen      = 0xc2
	vrhs          = 0xc3
	vdvs          = 0xc4
	frctrl2       = 0xc6
	pwctrl1       = 0xd0
	porctrl       = 0xb2
	gmctrp1       = 0xe0
	gmctrn1       = 0xe1
	slpout        = 0x11
	dispon        = 0x29
	ramwr         = 0x2c
	invon         = 0x21
	caset         = 0x2a
	raset         = 0x2b
)

const (
	rowOrder  = 0b10000000
	swapXY    = 0b00100000
	scanOrder = 0b00010000
)

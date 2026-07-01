//go:build arm64 && !purego

package dsp

import "unsafe"

func iht16x16_256AddAdstAdstNEON(input *int16, dest *byte, stride int) {
	var rowOutput [256]int16
	rowPtr := &rowOutput[0]
	noOutput := (*int16)(nil)

	iadst16x16_256AddHalf1DNEON(input, rowPtr, dest, stride, 0)
	iadst16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(input), 8*16*2)),
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 8*2)),
		dest, stride, 0)

	iadst16x16_256AddHalf1DNEON(rowPtr, noOutput, dest, stride, 0)
	iadst16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 16*8*2)),
		noOutput,
		(*byte)(unsafe.Add(unsafe.Pointer(dest), 8)),
		stride, 0)
}

// Names follow VP9 txType semantics; the first inverse half-pass operates in
// the opposite axis naming used by the ADST_DCT/DCT_ADST enum comments.
func iht16x16_256AddDctAdstNEON(input *int16, dest *byte, stride int) {
	var rowOutput [256]int16
	rowPtr := &rowOutput[0]
	noOutput := (*int16)(nil)

	iadst16x16_256AddHalf1DNEON(input, rowPtr, dest, stride, 0)
	iadst16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(input), 8*16*2)),
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 8*2)),
		dest, stride, 0)

	idct16x16_256AddHalf1DNEON(rowPtr, noOutput, dest, stride, 0)
	idct16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 16*8*2)),
		noOutput,
		(*byte)(unsafe.Add(unsafe.Pointer(dest), 8)),
		stride, 0)
}

func iht16x16_256AddAdstDctNEON(input *int16, dest *byte, stride int) {
	var rowOutput [256]int16
	rowPtr := &rowOutput[0]
	noOutput := (*int16)(nil)

	idct16x16_256AddHalf1DNEON(input, rowPtr, dest, stride, 0)
	idct16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(input), 8*16*2)),
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 8*2)),
		dest, stride, 0)

	iadst16x16_256AddHalf1DNEON(rowPtr, noOutput, dest, stride, 0)
	iadst16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 16*8*2)),
		noOutput,
		(*byte)(unsafe.Add(unsafe.Pointer(dest), 8)),
		stride, 0)
}

func idct16x16_256AddNEON(input *int16, dest *byte, stride int) {
	var rowOutput [256]int16
	rowPtr := &rowOutput[0]
	noOutput := (*int16)(nil)

	idct16x16_256AddHalf1DNEON(input, rowPtr, dest, stride, 0)
	idct16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(input), 8*16*2)),
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 8*2)),
		dest, stride, 0)

	idct16x16_256AddHalf1DNEON(rowPtr, noOutput, dest, stride, 0)
	idct16x16_256AddHalf1DNEON(
		(*int16)(unsafe.Add(unsafe.Pointer(rowPtr), 16*8*2)),
		noOutput,
		(*byte)(unsafe.Add(unsafe.Pointer(dest), 8)),
		stride, 0)
}

//go:noescape
func iadst16x16_256AddHalf1DNEON(input *int16, output *int16, dest *byte, stride int, highbd int)

//go:noescape
func idct16x16_256AddHalf1DNEON(input *int16, output *int16, dest *byte, stride int, highbd int)

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

//go:noescape
func iadst16x16_256AddHalf1DNEON(input *int16, output *int16, dest *byte, stride int, highbd int)

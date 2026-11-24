// SPDX-License-Identifier: BSD-2-Clause
// Extracted from goav1 at ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"simd/archsimd"
)

// A zero-padded eight-tap coefficient block is shared by VP8 and VP9.
type hbdTaps16 = [8]archsimd.Int16x8

// Read exactly eight bytes; the last codec row may have no trailing padding.
func u8Samples8(pix []byte) archsimd.Int16x8 { return widen8(load8(pix)) }

func u8SumRowX(pix []byte, t *hbdTaps16, n int) (lo, hi archsimd.Int32x4) {
	lo = archsimd.BroadcastInt32x4(0)
	hi = lo
	if len(pix) < 16 {
		for k := range n {
			lo, hi = mac(lo, hi, u8Samples8(pix[k:]), t[k])
		}
		return lo, hi
	}
	a := archsimd.LoadUint8x16(pix[:16])
	b := archsimd.Uint8x16{}
	lo, hi = mac(lo, hi, a.ExtendLo8ToUint16().BitsToInt16(), t[0])
	lo, hi = mac(lo, hi, b.ConcatShiftBytesRight(a, 1).ExtendLo8ToUint16().BitsToInt16(), t[1])
	lo, hi = mac(lo, hi, b.ConcatShiftBytesRight(a, 2).ExtendLo8ToUint16().BitsToInt16(), t[2])
	lo, hi = mac(lo, hi, b.ConcatShiftBytesRight(a, 3).ExtendLo8ToUint16().BitsToInt16(), t[3])
	lo, hi = mac(lo, hi, b.ConcatShiftBytesRight(a, 4).ExtendLo8ToUint16().BitsToInt16(), t[4])
	lo, hi = mac(lo, hi, b.ConcatShiftBytesRight(a, 5).ExtendLo8ToUint16().BitsToInt16(), t[5])
	lo, hi = mac(lo, hi, b.ConcatShiftBytesRight(a, 6).ExtendLo8ToUint16().BitsToInt16(), t[6])
	lo, hi = mac(lo, hi, b.ConcatShiftBytesRight(a, 7).ExtendLo8ToUint16().BitsToInt16(), t[7])
	return lo, hi
}

func u8SumColY(pix []byte, step int, t *hbdTaps16, n int) (lo, hi archsimd.Int32x4) {
	lo = archsimd.BroadcastInt32x4(0)
	hi = lo
	for k := range n {
		lo, hi = mac(lo, hi, u8Samples8(pix[k*step:]), t[k])
	}
	return lo, hi
}

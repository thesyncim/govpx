// SPDX-License-Identifier: BSD-2-Clause
// Transpose extracted from goav1 ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
)

func asI32(v archsimd.Int64x4) archsimd.Int32x8 { return v.AsInt32x8() }
func asI64(v archsimd.Int32x8) archsimd.Int64x4 { return v.AsInt64x4() }
func Transpose8(r0, r1, r2, r3, r4, r5, r6, r7 archsimd.Int32x8) (archsimd.Int32x8, archsimd.Int32x8, archsimd.Int32x8, archsimd.Int32x8, archsimd.Int32x8, archsimd.Int32x8, archsimd.Int32x8, archsimd.Int32x8) {
	a0 := r0.InterleaveLoGrouped(r1)
	a1 := r0.InterleaveHiGrouped(r1)
	a2 := r2.InterleaveLoGrouped(r3)
	a3 := r2.InterleaveHiGrouped(r3)
	a4 := r4.InterleaveLoGrouped(r5)
	a5 := r4.InterleaveHiGrouped(r5)
	a6 := r6.InterleaveLoGrouped(r7)
	a7 := r6.InterleaveHiGrouped(r7)

	b0 := asI32(asI64(a0).InterleaveLoGrouped(asI64(a2)))
	b1 := asI32(asI64(a0).InterleaveHiGrouped(asI64(a2)))
	b2 := asI32(asI64(a1).InterleaveLoGrouped(asI64(a3)))
	b3 := asI32(asI64(a1).InterleaveHiGrouped(asI64(a3)))
	b4 := asI32(asI64(a4).InterleaveLoGrouped(asI64(a6)))
	b5 := asI32(asI64(a4).InterleaveHiGrouped(asI64(a6)))
	b6 := asI32(asI64(a5).InterleaveLoGrouped(asI64(a7)))
	b7 := asI32(asI64(a5).InterleaveHiGrouped(asI64(a7)))

	// Output column k is the low 128-bit half of b_k joined with the low half
	// of b_(k+4) (column k, rows 0-7), and likewise for the high halves.
	return b0.ConcatPermute128Scalars(0, 2, b4), b1.ConcatPermute128Scalars(0, 2, b5), b2.ConcatPermute128Scalars(0, 2, b6), b3.ConcatPermute128Scalars(0, 2, b7), b0.ConcatPermute128Scalars(1, 3, b4), b1.ConcatPermute128Scalars(1, 3, b5), b2.ConcatPermute128Scalars(1, 3, b6), b3.ConcatPermute128Scalars(1, 3, b7)
}

func Load8(s []int16) archsimd.Int32x8 { return archsimd.LoadInt16x8(s).ExtendToInt32() }
func Store8(s []int16, v archsimd.Int32x8) {
	v = v.ShiftAllLeft(16).ShiftAllRight(16)
	v.GetLo().SaturateToInt16Concat(v.GetHi()).Store(s)
}
func LoadPixels8(s []byte) archsimd.Int32x8 {
	return load8(s).ExtendLo8ToUint16().BitsToInt16().ExtendToInt32()
}
func StorePixels8(s []byte, v archsimd.Int32x8) { store8(s, v.GetLo(), v.GetHi()) }
func Max8(v archsimd.Int32x8) int32             { return Max4(v.GetLo().Max(v.GetHi())) }

// FinishWide ends an AVX2 kernel before ordinary Go/SSE register moves.
func FinishWide() { archsimd.ClearAVXUpperBits() }

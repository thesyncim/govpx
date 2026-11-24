// SPDX-License-Identifier: BSD-2-Clause
// Transpose extracted from goav1 ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

func i32AsI16(v archsimd.Int32x4) archsimd.Int16x8 {
	return v.ToBits().ReshapeToUint16s().BitsToInt16()
}
func i16AsI32(v archsimd.Int16x8) archsimd.Int32x4 {
	return v.ToBits().ReshapeToUint32s().BitsToInt32()
}
func i64AsI16(v archsimd.Int64x2) archsimd.Int16x8 {
	return v.ToBits().ReshapeToUint16s().BitsToInt16()
}
func i16AsI64(v archsimd.Int16x8) archsimd.Int64x2 {
	return v.ToBits().ReshapeToUint64s().BitsToInt64()
}
func Transpose8Int16(l0, l1, l2, l3, l4, l5, l6, l7 archsimd.Int16x8) (archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8) {
	a0 := l0.InterleaveLo(l1)
	a1 := l0.InterleaveHi(l1)
	a2 := l2.InterleaveLo(l3)
	a3 := l2.InterleaveHi(l3)
	a4 := l4.InterleaveLo(l5)
	a5 := l4.InterleaveHi(l5)
	a6 := l6.InterleaveLo(l7)
	a7 := l6.InterleaveHi(l7)
	b0 := i32AsI16(i16AsI32(a0).InterleaveLo(i16AsI32(a2)))
	b1 := i32AsI16(i16AsI32(a0).InterleaveHi(i16AsI32(a2)))
	b2 := i32AsI16(i16AsI32(a1).InterleaveLo(i16AsI32(a3)))
	b3 := i32AsI16(i16AsI32(a1).InterleaveHi(i16AsI32(a3)))
	b4 := i32AsI16(i16AsI32(a4).InterleaveLo(i16AsI32(a6)))
	b5 := i32AsI16(i16AsI32(a4).InterleaveHi(i16AsI32(a6)))
	b6 := i32AsI16(i16AsI32(a5).InterleaveLo(i16AsI32(a7)))
	b7 := i32AsI16(i16AsI32(a5).InterleaveHi(i16AsI32(a7)))
	o0 := i64AsI16(i16AsI64(b0).InterleaveLo(i16AsI64(b4)))
	o1 := i64AsI16(i16AsI64(b0).InterleaveHi(i16AsI64(b4)))
	o2 := i64AsI16(i16AsI64(b1).InterleaveLo(i16AsI64(b5)))
	o3 := i64AsI16(i16AsI64(b1).InterleaveHi(i16AsI64(b5)))
	o4 := i64AsI16(i16AsI64(b2).InterleaveLo(i16AsI64(b6)))
	o5 := i64AsI16(i16AsI64(b2).InterleaveHi(i16AsI64(b6)))
	o6 := i64AsI16(i16AsI64(b3).InterleaveLo(i16AsI64(b7)))
	o7 := i64AsI16(i16AsI64(b3).InterleaveHi(i16AsI64(b7)))
	return o0, o1, o2, o3, o4, o5, o6, o7
}

// SPDX-License-Identifier: BSD-2-Clause
// Extracted from goav1 at ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

func pack2Rows(p []byte, stride int) archsimd.Uint8x16 {
	lo := binary.LittleEndian.Uint64(p)
	hi := binary.LittleEndian.Uint64(p[stride:])
	return archsimd.BroadcastUint64x2(lo).SetElem(1, hi).ReshapeToUint8s()
}

func sad8x8Slice(src []byte, srcStride int, ref []byte, refStride int) int {
	acc := archsimd.BroadcastUint16x8(0)
	for row := 0; row < 8; row += 2 {
		s := pack2Rows(src[row*srcStride:], srcStride)
		r := pack2Rows(ref[row*refStride:], refStride)
		acc = acc.Add(widen16(absDiffU8x16(s, r)))
	}
	return int(acc.ReduceSum())
}

func sad16ColsDualSlice(src []byte, srcStride int, ref []byte, refStride int, h int) int {
	even := archsimd.BroadcastUint16x8(0)
	odd := archsimd.BroadcastUint16x8(0)
	for row := 0; row < h; row += 2 {
		even = absAcc16(even, archsimd.LoadUint8x16(src[row*srcStride:]), archsimd.LoadUint8x16(ref[row*refStride:]))
		odd = absAcc16(odd, archsimd.LoadUint8x16(src[(row+1)*srcStride:]), archsimd.LoadUint8x16(ref[(row+1)*refStride:]))
	}
	return sumU16(even) + sumU16(odd)
}

func sad16ColsX4Slice(src, p0, p1, p2, p3 []byte, srcStride, refStride int, h int) (int, int, int, int) {
	a0 := archsimd.BroadcastUint16x8(0)
	a1 := archsimd.BroadcastUint16x8(0)
	a2 := archsimd.BroadcastUint16x8(0)
	a3 := archsimd.BroadcastUint16x8(0)
	for row := 0; row < h; row++ {
		o := row * refStride
		s := archsimd.LoadUint8x16(src[row*srcStride:])
		a0 = absAcc16(a0, s, archsimd.LoadUint8x16(p0[o:]))
		a1 = absAcc16(a1, s, archsimd.LoadUint8x16(p1[o:]))
		a2 = absAcc16(a2, s, archsimd.LoadUint8x16(p2[o:]))
		a3 = absAcc16(a3, s, archsimd.LoadUint8x16(p3[o:]))
	}
	return sumU16(a0), sumU16(a1), sumU16(a2), sumU16(a3)
}

func absDiffU8x16(a, b archsimd.Uint8x16) archsimd.Uint8x16 {
	return a.SubSaturated(b).Or(b.SubSaturated(a))
}

func widen16(absd archsimd.Uint8x16) archsimd.Uint16x8 {
	return absd.ExtendLo8ToUint16().Add(absd.HiToLo().ExtendLo8ToUint16())
}

func absAcc16(acc archsimd.Uint16x8, s, r archsimd.Uint8x16) archsimd.Uint16x8 {
	return acc.Add(widen16(absDiffU8x16(s, r)))
}

func sumU16(acc archsimd.Uint16x8) int {
	return int(acc.ExtendLo4ToUint32().Add(acc.HiToLo().ExtendLo4ToUint32()).ReduceSum())
}

func sad16LimitSlice(src []byte, ss int, ref []byte, rs, h, limit int) int {
	var sum archsimd.Uint16x8
	for y := range h {
		a := archsimd.LoadUint8x16(src[y*ss:])
		b := archsimd.LoadUint8x16(ref[y*rs:])
		sum = sum.Add(absPairs(a, b))
		if value := sumPairs(sum); value > limit {
			return value
		}
	}
	return sumPairs(sum)
}

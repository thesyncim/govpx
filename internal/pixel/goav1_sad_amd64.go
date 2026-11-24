// SPDX-License-Identifier: BSD-2-Clause
// Extracted from goav1 at ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && amd64 && !purego

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
	acc := archsimd.BroadcastUint64x2(0)
	for row := 0; row < 8; row += 2 {
		s := pack2Rows(src[row*srcStride:], srcStride)
		r := pack2Rows(ref[row*refStride:], refStride)
		acc = acc.Add(s.SumOf8AbsDiff(r))
	}
	return sumU64(acc)
}

func sad16ColsDualSlice(src []byte, srcStride int, ref []byte, refStride int, h int) int {
	acc := archsimd.BroadcastUint64x2(0)
	for row := 0; row < h; row++ {
		acc = acc.Add(archsimd.LoadUint8x16(src[row*srcStride:]).SumOf8AbsDiff(archsimd.LoadUint8x16(ref[row*refStride:])))
	}
	return sumU64(acc)
}

func sad16ColsX4Slice(src, p0, p1, p2, p3 []byte, srcStride, refStride int, h int) (int, int, int, int) {
	a0 := archsimd.BroadcastUint64x2(0)
	a1 := archsimd.BroadcastUint64x2(0)
	a2 := archsimd.BroadcastUint64x2(0)
	a3 := archsimd.BroadcastUint64x2(0)
	for row := 0; row < h; row++ {
		o := row * refStride
		s := archsimd.LoadUint8x16(src[row*srcStride:])
		a0 = a0.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p0[o:])))
		a1 = a1.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p1[o:])))
		a2 = a2.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p2[o:])))
		a3 = a3.Add(s.SumOf8AbsDiff(archsimd.LoadUint8x16(p3[o:])))
	}
	return sumU64(a0), sumU64(a1), sumU64(a2), sumU64(a3)
}

func sumU64(acc archsimd.Uint64x2) int {
	return int(acc.GetElem(0) + acc.GetElem(1))
}

// Preserve the exact first-row-over-limit result while using PSADBW's two
// independent eight-byte sums instead of byte widening and reduction.
func sad16LimitSlice(src []byte, ss int, ref []byte, rs, h, limit int) int {
	var acc archsimd.Uint64x2
	value := 0
	for y := range h {
		acc = acc.Add(archsimd.LoadUint8x16(src[y*ss:]).SumOf8AbsDiff(archsimd.LoadUint8x16(ref[y*rs:])))
		value = sumU64(acc)
		if value > limit {
			return value
		}
	}
	return value
}

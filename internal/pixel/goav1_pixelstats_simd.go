// SPDX-License-Identifier: BSD-2-Clause
// Extracted from goav1 at ff46346e73f063ff1ea3a8d7f06ee26cd9baaad4.
// See UPSTREAM.md and ../../third_party/goav1/LICENSE.
//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

func pixelDiffAcc(sse, sum archsimd.Int32x4, a, b archsimd.Uint8x16) (archsimd.Int32x4, archsimd.Int32x4) {
	lo := a.ExtendLo8ToUint16().BitsToInt16().Sub(b.ExtendLo8ToUint16().BitsToInt16())
	hi := widenHi8(a).BitsToInt16().Sub(widenHi8(b).BitsToInt16())
	sse = sse.Add(square8(lo)).Add(square8(hi))
	sum = sum.Add(sumDiff16(lo.Add(hi)))
	return sse, sum
}

func reducePixelAcc(sse, sum archsimd.Int32x4) (total, signed pixelStatsReduction) {
	// ARM reduces before crossing to scalar registers. Keep the original
	// scalar extraction on amd64, where native short-row measurements win.
	if vectorPixelAccReduction {
		return pixelStatsReduction(sum32(sse)), pixelStatsReduction(sum32(sum))
	}
	total = pixelStatsReduction(sse.GetElem(0)) + pixelStatsReduction(sse.GetElem(1)) + pixelStatsReduction(sse.GetElem(2)) + pixelStatsReduction(sse.GetElem(3))
	signed = pixelStatsReduction(sum.GetElem(0)) + pixelStatsReduction(sum.GetElem(1)) + pixelStatsReduction(sum.GetElem(2)) + pixelStatsReduction(sum.GetElem(3))
	return total, signed
}

func pixelStatsRows16(src []byte, srcStride int, ref []byte, refStride int, w, h int) (sse uint32, sum int32) {
	if (uint(srcStride)|uint(refStride))>>57 == 0 && h > 0 && w == 32 && h <= 64 {
		_ = src[(h-1)*srcStride+31]
		_ = ref[(h-1)*refStride+31]
		var s0, s1, sumAcc archsimd.Int32x4
		for r := range h {
			srow := src[r*srcStride : r*srcStride+32]
			rrow := ref[r*refStride : r*refStride+32]
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(srow[0:16]))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(rrow[0:16]))
			lo0 := a0.ExtendLo8ToUint16().BitsToInt16().Sub(b0.ExtendLo8ToUint16().BitsToInt16())
			hi0 := widenHi8(a0).BitsToInt16().Sub(widenHi8(b0).BitsToInt16())
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(srow[16:32]))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(rrow[16:32]))
			lo1 := a1.ExtendLo8ToUint16().BitsToInt16().Sub(b1.ExtendLo8ToUint16().BitsToInt16())
			hi1 := widenHi8(a1).BitsToInt16().Sub(widenHi8(b1).BitsToInt16())
			sumAcc = sumAcc.Add(sumDiff16(lo0.Add(hi0).Add(lo1).Add(hi1)))
			s0 = s0.Add(square8(lo0)).Add(square8(lo1))
			s1 = s1.Add(square8(hi0)).Add(square8(hi1))
		}
		total, signed := reducePixelAcc(s0.Add(s1), sumAcc)
		return uint32(total), int32(signed)
	}
	if (uint(srcStride)|uint(refStride))>>57 == 0 && h > 0 && w == 64 && h <= 64 {
		_ = src[(h-1)*srcStride+63]
		_ = ref[(h-1)*refStride+63]
		var s0, s1, sumAcc archsimd.Int32x4
		for r := range h {
			srow := src[r*srcStride : r*srcStride+64]
			rrow := ref[r*refStride : r*refStride+64]
			var rowSum archsimd.Int16x8
			for c := 0; c < 64; c += 16 {
				a := archsimd.LoadUint8x16Array((*[16]byte)(srow[c : c+16]))
				b := archsimd.LoadUint8x16Array((*[16]byte)(rrow[c : c+16]))
				lo := a.ExtendLo8ToUint16().BitsToInt16().Sub(b.ExtendLo8ToUint16().BitsToInt16())
				hi := widenHi8(a).BitsToInt16().Sub(widenHi8(b).BitsToInt16())
				rowSum = rowSum.Add(lo).Add(hi)
				s0 = s0.Add(square8(lo))
				s1 = s1.Add(square8(hi))
			}
			sumAcc = sumAcc.Add(sumDiff16(rowSum))
		}
		total, signed := reducePixelAcc(s0.Add(s1), sumAcc)
		return uint32(total), int32(signed)
	}
	sseAcc := archsimd.BroadcastInt32x4(0)
	sumAcc := archsimd.BroadcastInt32x4(0)
	for r := range h {
		srow := src[r*srcStride : r*srcStride+w]
		rrow := ref[r*refStride : r*refStride+w]
		for c := 0; c < w; c += 16 {
			sseAcc, sumAcc = pixelDiffAcc(sseAcc, sumAcc, archsimd.LoadUint8x16(srow[c:c+16]), archsimd.LoadUint8x16(rrow[c:c+16]))
		}
	}
	total, signed := reducePixelAcc(sseAcc, sumAcc)
	return uint32(total), int32(signed)
}

func pixelStatsPairs8(src []byte, srcStride int, ref []byte, refStride int, h int) (sse uint32, sum int32) {
	sseAcc := archsimd.BroadcastInt32x4(0)
	sumAcc := archsimd.BroadcastInt32x4(0)
	r := 0
	for ; r+2 <= h; r += 2 {
		sseAcc, sumAcc = pixelDiffAcc(sseAcc, sumAcc, packRows8(src, r*srcStride, srcStride), packRows8(ref, r*refStride, refStride))
	}
	if r < h {
		srow, rrow := src[r*srcStride:], ref[r*refStride:]
		if len(srow) < 8 || len(rrow) < 8 {
			// Preserve the original scalar access/panic order for short rows.
			total, signed := reducePixelAcc(sseAcc, sumAcc)
			t, sg := pixelStatsPureGo(srow, srcStride, rrow, refStride, 8, 1)
			return uint32(total + pixelStatsReduction(t)), int32(signed + pixelStatsReduction(sg))
		}
		// The final eight pixels need only one exact low-half packet.
		a := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(srow)).ReshapeToUint8s()
		b := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(rrow)).ReshapeToUint8s()
		delta := a.ExtendLo8ToUint16().BitsToInt16().Sub(b.ExtendLo8ToUint16().BitsToInt16())
		sseAcc = sseAcc.Add(square8(delta))
		sumAcc = sumAcc.Add(sumDiff16(delta))
	}
	total, signed := reducePixelAcc(sseAcc, sumAcc)
	return uint32(total), int32(signed)
}

func packRows8(p []byte, off, stride int) archsimd.Uint8x16 {
	lo := binary.LittleEndian.Uint64(p[off:])
	hi := binary.LittleEndian.Uint64(p[off+stride:])
	return archsimd.BroadcastUint64x2(lo).SetElem(1, hi).ReshapeToUint8s()
}

// Four exact four-byte row loads fill one byte vector, eliminating the
// per-row widening/setup of the 4x4 and 4x8 variance hot paths.
func packRows4(p []byte, off, stride int) archsimd.Uint8x16 {
	return archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(p[off:])).SetElem(1, binary.LittleEndian.Uint32(p[off+stride:])).SetElem(2, binary.LittleEndian.Uint32(p[off+2*stride:])).SetElem(3, binary.LittleEndian.Uint32(p[off+3*stride:])).ReshapeToUint8s()
}
func pixelStatsQuads4(src []byte, ss int, ref []byte, rs, h int) (uint32, int32) {
	var sse, sum archsimd.Int32x4
	for y := 0; y < h; y += 4 {
		sse, sum = pixelDiffAcc(sse, sum, packRows4(src, y*ss, ss), packRows4(ref, y*rs, rs))
	}
	return uint32(sum32(sse)), sum32(sum)
}

func pixelStatsSIMD(src []byte, srcStride int, ref []byte, refStride int, w, h int) (sse uint32, sum int32) {
	switch {
	case narrowPixelStats16 && w == 16 && h > 0 && h <= 64:
		return pixelStats16Narrow(src, srcStride, ref, refStride, h)
	case w == 4 && h%4 == 0:
		return pixelStatsQuads4(src, srcStride, ref, refStride, h)
	case w == 8:
		return pixelStatsPairs8(src, srcStride, ref, refStride, h)
	case w >= 16 && w&15 == 0:
		return pixelStatsRows16(src, srcStride, ref, refStride, w, h)
	}
	return pixelStatsPureGo(src, srcStride, ref, refStride, w, h)
}

// pixelStatsPureGo is govpx's glue for the extracted goav1 fallback contract.
func pixelStatsPureGo(src []byte, ss int, ref []byte, rs, w, h int) (uint32, int32) {
	sum, sse := StatsScalar(src, ss, ref, rs, w, h)
	return sse, int32(sum)
}

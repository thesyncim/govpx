//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
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
		var sum16 archsimd.Int16x8
		for r := range h {
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16]))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(ref[0:16]))
			lo0 := a0.ExtendLo8ToUint16().BitsToInt16().Sub(b0.ExtendLo8ToUint16().BitsToInt16())
			hi0 := widenHi8(a0).BitsToInt16().Sub(widenHi8(b0).BitsToInt16())
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[16:32]))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(ref[16:32]))
			lo1 := a1.ExtendLo8ToUint16().BitsToInt16().Sub(b1.ExtendLo8ToUint16().BitsToInt16())
			hi1 := widenHi8(a1).BitsToInt16().Sub(widenHi8(b1).BitsToInt16())
			// Max per-lane abs after 16 rows: 16 * 4 * 255 = 16320 < 32767.
			sum16 = sum16.Add(lo0).Add(hi0).Add(lo1).Add(hi1)
			s0 = s0.Add(square8(lo0)).Add(square8(lo1))
			s1 = s1.Add(square8(hi0)).Add(square8(hi1))
			if r&15 == 15 {
				sumAcc = sumAcc.Add(sumDiff16(sum16))
				sum16 = archsimd.Int16x8{}
			}
			if r+1 < h {
				src = src[srcStride:]
				ref = ref[refStride:]
			}
		}
		if h&15 != 0 {
			sumAcc = sumAcc.Add(sumDiff16(sum16))
		}
		total, signed := reducePixelAcc(s0.Add(s1), sumAcc)
		return uint32(total), int32(signed)
	}
	if (uint(srcStride)|uint(refStride))>>57 == 0 && h > 0 && w == 64 && h <= 64 {
		_ = src[(h-1)*srcStride+63]
		_ = ref[(h-1)*refStride+63]
		var s0, s1, sumAcc archsimd.Int32x4
		var sum16 archsimd.Int16x8
		for r := range h {
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16]))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(ref[0:16]))
			lo0 := a0.ExtendLo8ToUint16().BitsToInt16().Sub(b0.ExtendLo8ToUint16().BitsToInt16())
			hi0 := widenHi8(a0).BitsToInt16().Sub(widenHi8(b0).BitsToInt16())
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[16:32]))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(ref[16:32]))
			lo1 := a1.ExtendLo8ToUint16().BitsToInt16().Sub(b1.ExtendLo8ToUint16().BitsToInt16())
			hi1 := widenHi8(a1).BitsToInt16().Sub(widenHi8(b1).BitsToInt16())
			a2 := archsimd.LoadUint8x16Array((*[16]byte)(src[32:48]))
			b2 := archsimd.LoadUint8x16Array((*[16]byte)(ref[32:48]))
			lo2 := a2.ExtendLo8ToUint16().BitsToInt16().Sub(b2.ExtendLo8ToUint16().BitsToInt16())
			hi2 := widenHi8(a2).BitsToInt16().Sub(widenHi8(b2).BitsToInt16())
			a3 := archsimd.LoadUint8x16Array((*[16]byte)(src[48:64]))
			b3 := archsimd.LoadUint8x16Array((*[16]byte)(ref[48:64]))
			lo3 := a3.ExtendLo8ToUint16().BitsToInt16().Sub(b3.ExtendLo8ToUint16().BitsToInt16())
			hi3 := widenHi8(a3).BitsToInt16().Sub(widenHi8(b3).BitsToInt16())
			// Max per-lane abs after 8 rows: 8 * 8 * 255 = 16320 < 32767.
			sum16 = sum16.Add(lo0).Add(hi0).Add(lo1).Add(hi1).Add(lo2).Add(hi2).Add(lo3).Add(hi3)
			s0 = s0.Add(square8(lo0)).Add(square8(lo1)).Add(square8(lo2)).Add(square8(lo3))
			s1 = s1.Add(square8(hi0)).Add(square8(hi1)).Add(square8(hi2)).Add(square8(hi3))
			if r&7 == 7 {
				sumAcc = sumAcc.Add(sumDiff16(sum16))
				sum16 = archsimd.Int16x8{}
			}
			if r+1 < h {
				src = src[srcStride:]
				ref = ref[refStride:]
			}
		}
		if h&7 != 0 {
			sumAcc = sumAcc.Add(sumDiff16(sum16))
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

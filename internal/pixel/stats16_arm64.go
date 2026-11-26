//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

type pixelStatsReduction = int32

const vectorPixelAccReduction = true

// pixelStats16Narrow handles sixteen columns and 1..64 rows. Each signed
// sum lane receives two differences per row: |sum| <= 2*255*64 = 32640.
// Keeping sums in int16 eliminates widening in every row. Squared errors
// stay in separate int32 accumulators and reduce to at most 66585600.
func pixelStats16Narrow(src []byte, ss int, ref []byte, rs, h int) (uint32, int32) {
	var s0, s1 archsimd.Int32x4
	var sum archsimd.Int16x8
	// Reject negative or implausibly large strides before the final-row
	// bounds checks. At h<=64 and stride<2^57, (h-1)*stride+15 fits int64.
	// The two checks then cover every exact 16-byte load, including zero
	// strides and overlapping read-only rows; no padding is required.
	if (uint(ss)|uint(rs))>>57 != 0 {
		return pixelStatsRows16(src, ss, ref, rs, 16, h)
	}
	_ = src[(h-1)*ss+15]
	_ = ref[(h-1)*rs+15]
	if h == 16 && ss >= 16 && rs >= 16 && len(src) >= 16*ss && len(ref) >= 16*rs {
		// Unroll 4 rows per iteration with constant trip count and independent
		// accumulators to expose full NEON ILP across vector pipes and cut loop
		// and slice-advance overhead by 4x.
		var s2, s3 archsimd.Int32x4
		var sum1 archsimd.Int16x8
		ss2, rs2 := ss*2, rs*2
		ss3, rs3 := ss*3, rs*3
		ss4, rs4 := ss*4, rs*4
		for range 4 {
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[:16]))
			b0 := archsimd.LoadUint8x16Array((*[16]byte)(ref[:16]))
			lo0 := a0.ExtendLo8ToUint16().BitsToInt16().Sub(b0.ExtendLo8ToUint16().BitsToInt16())
			hi0 := a0.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(b0.HiToLo().ExtendLo8ToUint16().BitsToInt16())
			sum = sum.Add(lo0).Add(hi0)
			s0 = s0.Add(square8(lo0))
			s1 = s1.Add(square8(hi0))

			a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[ss : ss+16]))
			b1 := archsimd.LoadUint8x16Array((*[16]byte)(ref[rs : rs+16]))
			lo1 := a1.ExtendLo8ToUint16().BitsToInt16().Sub(b1.ExtendLo8ToUint16().BitsToInt16())
			hi1 := a1.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(b1.HiToLo().ExtendLo8ToUint16().BitsToInt16())
			sum1 = sum1.Add(lo1).Add(hi1)
			s2 = s2.Add(square8(lo1))
			s3 = s3.Add(square8(hi1))

			a2 := archsimd.LoadUint8x16Array((*[16]byte)(src[ss2 : ss2+16]))
			b2 := archsimd.LoadUint8x16Array((*[16]byte)(ref[rs2 : rs2+16]))
			lo2 := a2.ExtendLo8ToUint16().BitsToInt16().Sub(b2.ExtendLo8ToUint16().BitsToInt16())
			hi2 := a2.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(b2.HiToLo().ExtendLo8ToUint16().BitsToInt16())
			sum = sum.Add(lo2).Add(hi2)
			s0 = s0.Add(square8(lo2))
			s1 = s1.Add(square8(hi2))

			a3 := archsimd.LoadUint8x16Array((*[16]byte)(src[ss3 : ss3+16]))
			b3 := archsimd.LoadUint8x16Array((*[16]byte)(ref[rs3 : rs3+16]))
			lo3 := a3.ExtendLo8ToUint16().BitsToInt16().Sub(b3.ExtendLo8ToUint16().BitsToInt16())
			hi3 := a3.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(b3.HiToLo().ExtendLo8ToUint16().BitsToInt16())
			sum1 = sum1.Add(lo3).Add(hi3)
			s2 = s2.Add(square8(lo3))
			s3 = s3.Add(square8(hi3))

			src = src[ss4:]
			ref = ref[rs4:]
		}
		return uint32(sum32(s0.Add(s1).Add(s2.Add(s3)))), sum32(sumDiff16(sum.Add(sum1)))
	}
	for y := range h {
		a, b := archsimd.LoadUint8x16(src[y*ss:]), archsimd.LoadUint8x16(ref[y*rs:])
		lo := a.ExtendLo8ToUint16().BitsToInt16().Sub(b.ExtendLo8ToUint16().BitsToInt16())
		hi := widenHi8(a).BitsToInt16().Sub(widenHi8(b).BitsToInt16())
		sum = sum.Add(lo).Add(hi)
		s0 = s0.Add(square8(lo))
		s1 = s1.Add(square8(hi))
	}
	return uint32(sum32(s0.Add(s1))), sum32(sumDiff16(sum))
}

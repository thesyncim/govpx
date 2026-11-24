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

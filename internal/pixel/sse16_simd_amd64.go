//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// SSE16 widens a complete sixteen-pixel row into AVX2 int16 lanes. Two
// squared differences per dot-product lane accumulate modulo2^32.
func SSE16(src []byte, ss int, ref []byte, rs, h int) uint32 {
	if !Enabled {
		return SSE16Scalar(src, ss, ref, rs, h)
	}
	if h <= 0 {
		return 0
	}
	defer archsimd.ClearAVXUpperBits()
	var acc archsimd.Int32x8
	for y := range h {
		a := archsimd.LoadUint8x16(src[y*ss:]).ExtendToUint16().BitsToInt16()
		b := archsimd.LoadUint8x16(ref[y*rs:]).ExtendToUint16().BitsToInt16()
		d := a.Sub(b)
		acc = acc.Add(d.DotProductPairs(d))
	}
	return uint32(sum32(acc.GetLo().Add(acc.GetHi())))
}

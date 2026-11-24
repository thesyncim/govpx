//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// Preserve the existing wrapping int32 accumulators with twice the packet size.
func vectorVarianceWideR2(ref, src []int16, n, shift int) (int, bool) {
	if n <= 0 || n%8 != 0 || len(ref) < n || len(src) < n {
		return 0, false
	}
	var sum, sse archsimd.Int32x8
	for i := 0; i < n; i += 8 {
		a := archsimd.LoadInt16x8(ref[i:]).ExtendToInt32()
		b := archsimd.LoadInt16x8(src[i:]).ExtendToInt32()
		d := a.Sub(b)
		sum = sum.Add(d)
		sse = sse.Add(d.Mul(d))
	}
	m := int(sum32(sum.GetLo().Add(sum.GetHi())))
	value := int(sum32(sse.GetLo().Add(sse.GetHi()))) - (m * m >> uint(shift))
	archsimd.ClearAVXUpperBits()
	return value, true
}

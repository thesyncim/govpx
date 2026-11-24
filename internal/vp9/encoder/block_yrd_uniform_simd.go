//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import "simd/archsimd"

// The BlockYrd scoring caller proves complete residual rows, disjoint scratch,
// and the bounded quantizer contract documented by blockYrdQuantScore.
func blockYrdUniformScore(input []int16, stride, n int, r, q, d [2]int16) (int, uint32, bool) {
	v := input[0]
	same := archsimd.BroadcastInt16x8(v)
	for y := 0; y < n; y++ {
		row := input[y*stride:]
		for x := 0; x < n; x += 8 {
			c := archsimd.LoadInt16x8Array((*[8]int16)(row[x : x+8]))
			bits := c.ToBits().Xor(same.ToBits()).ReshapeToUint64s()
			if bits.GetElem(0)|bits.GetElem(1) != 0 {
				return 0, 0, false
			}
		}
	}
	dc := int(v) * 64
	if n == 16 {
		dc *= 2
	}
	if dc < 0 {
		dc = -dc
	}
	a := min(dc+int(r[0]), 32767)
	rate := a * int(q[0]) >> 16
	diff := dc - rate*int(d[0])
	return rate, uint32(diff * diff), true
}

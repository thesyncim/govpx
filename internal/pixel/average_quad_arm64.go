//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// AverageQuad16 computes the four independently rounded 8x8 averages.
// Each half-column accumulates eight bytes, at most 2040 per uint16 lane.
func AverageQuad16(src []byte, ss int, out *[4]int32) bool {
	if !Enabled || ss < 0 || len(src) < 16 || ss > (len(src)-16)/15 {
		return false
	}
	_ = src[15*ss+15]
	var a, b, c, d archsimd.Uint16x8
	so, bo := 0, 8*ss
	ss2 := 2 * ss
	for range 4 {
		t0 := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
		t1 := archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss : so+ss+16]))
		b0 := archsimd.LoadUint8x16Array((*[16]byte)(src[bo : bo+16]))
		b1 := archsimd.LoadUint8x16Array((*[16]byte)(src[bo+ss : bo+ss+16]))
		a = a.Add(t0.ExtendLo8ToUint16()).Add(t1.ExtendLo8ToUint16())
		b = b.Add(t0.HiToLo().ExtendLo8ToUint16()).Add(t1.HiToLo().ExtendLo8ToUint16())
		c = c.Add(b0.ExtendLo8ToUint16()).Add(b1.ExtendLo8ToUint16())
		d = d.Add(b0.HiToLo().ExtendLo8ToUint16()).Add(b1.HiToLo().ExtendLo8ToUint16())
		so += ss2
		bo += ss2
	}
	out[0] = int32((averageReduceARM8(a) + 32) >> 6)
	out[1] = int32((averageReduceARM8(b) + 32) >> 6)
	out[2] = int32((averageReduceARM8(c) + 32) >> 6)
	out[3] = int32((averageReduceARM8(d) + 32) >> 6)
	return true
}

func averageReduceARM8(v archsimd.Uint16x8) int { return int(v.ReduceSum()) }

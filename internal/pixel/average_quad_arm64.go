//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// AverageQuad16 computes the four independently rounded 8x8 averages.
// Each half-column accumulates eight bytes, at most 2040 per uint16 lane.
func AverageQuad16(src []byte, ss int, out *[4]int32) bool {
	if !Enabled || ss < 0 || len(src) < 16 || ss > (len(src)-16)/15 {
		return false
	}
	var a, b, c, d archsimd.Uint16x8
	for y := range 8 {
		top := archsimd.LoadUint8x16(src[y*ss:])
		bottom := archsimd.LoadUint8x16(src[(y+8)*ss:])
		a = a.Add(top.ExtendLo8ToUint16())
		b = b.Add(highBytes(top).ExtendLo8ToUint16())
		c = c.Add(bottom.ExtendLo8ToUint16())
		d = d.Add(highBytes(bottom).ExtendLo8ToUint16())
	}
	out[0] = int32((averageReduceARM8(a) + 32) >> 6)
	out[1] = int32((averageReduceARM8(b) + 32) >> 6)
	out[2] = int32((averageReduceARM8(c) + 32) >> 6)
	out[3] = int32((averageReduceARM8(d) + 32) >> 6)
	return true
}

func averageReduceARM8(v archsimd.Uint16x8) int { return int(v.ReduceSum()) }

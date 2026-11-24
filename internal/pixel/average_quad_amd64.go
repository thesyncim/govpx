//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// Two PSADBW accumulators keep the left/right 8x8 totals independent.
func AverageQuad16(src []byte, ss int, out *[4]int32) bool {
	if !Enabled || ss < 0 || len(src) < 16 || ss > (len(src)-16)/15 {
		return false
	}
	var top, bottom archsimd.Uint64x2
	zero := archsimd.Uint8x16{}
	for y := range 8 {
		top = top.Add(archsimd.LoadUint8x16(src[y*ss:]).SumOf8AbsDiff(zero))
		bottom = bottom.Add(archsimd.LoadUint8x16(src[(y+8)*ss:]).SumOf8AbsDiff(zero))
	}
	out[0] = int32((top.GetElem(0) + 32) >> 6)
	out[1] = int32((top.GetElem(1) + 32) >> 6)
	out[2] = int32((bottom.GetElem(0) + 32) >> 6)
	out[3] = int32((bottom.GetElem(1) + 32) >> 6)
	return true
}

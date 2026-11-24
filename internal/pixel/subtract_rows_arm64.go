//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// Sixteen-column residual layout adapted from goav1 residualBlockSIMD,
// rdstats_gosimd.go at 0f40b2bd848d8c5c7bdbaa016f2ced3085f7fc7a.
func subtract16Rows(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) bool {
	var bits archsimd.Int16x8
	for y := range h {
		srow, prow, orow := src[y*ss:], pred[y*ps:], out[y*os:]
		for x := 0; x < w; x += 16 {
			a := archsimd.LoadUint8x16(srow[x:])
			b := archsimd.LoadUint8x16(prow[x:])
			lo := widen8(a).Sub(widen8(b))
			hi := widenHi8(a).BitsToInt16().Sub(widenHi8(b).BitsToInt16())
			lo.Store(orow[x:])
			hi.Store(orow[x+8:])
			bits = bits.Or(lo).Or(hi)
		}
	}
	q := bits.ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}

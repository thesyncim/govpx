//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

func subtract16Rows(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) bool {
	defer archsimd.ClearAVXUpperBits()
	var bits archsimd.Int16x16
	for y := range h {
		srow, prow, orow := src[y*ss:], pred[y*ps:], out[y*os:]
		for x := 0; x < w; x += 16 {
			a := archsimd.LoadUint8x16(srow[x:])
			b := archsimd.LoadUint8x16(prow[x:])
			d := a.ExtendToUint16().BitsToInt16().Sub(b.ExtendToUint16().BitsToInt16())
			d.Store(orow[x:])
			bits = bits.Or(d)
		}
	}
	q := bits.GetLo().Or(bits.GetHi()).ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}

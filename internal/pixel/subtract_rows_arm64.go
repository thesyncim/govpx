//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// Sixteen-column residual layout adapted from goav1 residualBlockSIMD,
// rdstats_gosimd.go at 0f40b2bd848d8c5c7bdbaa016f2ced3085f7fc7a.
func subtract16Rows(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) bool {
	if h <= 0 || w <= 0 {
		return false
	}
	_ = src[(h-1)*ss+w-1]
	_ = pred[(h-1)*ps+w-1]
	_ = out[(h-1)*os+w-1]
	if w == 16 && h == 16 {
		var b0, b1 archsimd.Int16x8
		so, po, oo := 0, 0, 0
		ss2, ps2, os2 := 2*ss, 2*ps, 2*os
		for range 8 {
			a0 := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
			p0 := archsimd.LoadUint8x16Array((*[16]byte)(pred[po : po+16]))
			a1 := archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss : so+ss+16]))
			p1 := archsimd.LoadUint8x16Array((*[16]byte)(pred[po+ps : po+ps+16]))
			lo0 := a0.ExtendLo8ToUint16().BitsToInt16().Sub(p0.ExtendLo8ToUint16().BitsToInt16())
			hi0 := a0.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(p0.HiToLo().ExtendLo8ToUint16().BitsToInt16())
			lo1 := a1.ExtendLo8ToUint16().BitsToInt16().Sub(p1.ExtendLo8ToUint16().BitsToInt16())
			hi1 := a1.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(p1.HiToLo().ExtendLo8ToUint16().BitsToInt16())
			lo0.StoreArray((*[8]int16)(out[oo : oo+8]))
			hi0.StoreArray((*[8]int16)(out[oo+8 : oo+16]))
			lo1.StoreArray((*[8]int16)(out[oo+os : oo+os+8]))
			hi1.StoreArray((*[8]int16)(out[oo+os+8 : oo+os+16]))
			b0 = b0.Or(lo0).Or(hi0)
			b1 = b1.Or(lo1).Or(hi1)
			so += ss2
			po += ps2
			oo += os2
		}
		q := b0.Or(b1).ToBits().ReshapeToUint64s()
		return q.GetElem(0)|q.GetElem(1) != 0
	}
	var bits archsimd.Int16x8
	so, po, oo := 0, 0, 0
	for range h {
		for x := 0; x < w; x += 16 {
			a := archsimd.LoadUint8x16Array((*[16]byte)(src[so+x : so+x+16]))
			b := archsimd.LoadUint8x16Array((*[16]byte)(pred[po+x : po+x+16]))
			lo := a.ExtendLo8ToUint16().BitsToInt16().Sub(b.ExtendLo8ToUint16().BitsToInt16())
			hi := a.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(b.HiToLo().ExtendLo8ToUint16().BitsToInt16())
			lo.StoreArray((*[8]int16)(out[oo+x : oo+x+8]))
			hi.StoreArray((*[8]int16)(out[oo+x+8 : oo+x+16]))
			bits = bits.Or(lo).Or(hi)
		}
		so += ss
		po += ps
		oo += os
	}
	q := bits.ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}

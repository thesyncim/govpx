//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// Sixteen-column residual layout adapted from goav1 residualBlockSIMD,
// rdstats_gosimd.go at 0f40b2bd848d8c5c7bdbaa016f2ced3085f7fc7a.
func subtract16Rows(sp unsafe.Pointer, ss int, pp unsafe.Pointer, ps int, op unsafe.Pointer, os, w, h int) bool {
	var bits archsimd.Int16x8
	for {
		for x := 0; x < w; x += 16 {
			a := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, x)))
			b := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, x)))
			lo := widen8(a).Sub(widen8(b))
			hi := widenHi8(a).BitsToInt16().Sub(widenHi8(b).BitsToInt16())
			lo.StoreArray((*[8]int16)(unsafe.Add(op, 2*x)))
			hi.StoreArray((*[8]int16)(unsafe.Add(op, 2*(x+8))))
			bits = bits.Or(lo).Or(hi)
		}
		h--
		if h == 0 {
			break
		}
		// Advance only when another row remains; each pointer stays within
		// its validated object even with an unused large final stride.
		sp = unsafe.Add(sp, ss)
		pp = unsafe.Add(pp, ps)
		op = unsafe.Add(op, 2*os)
	}
	q := bits.ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}

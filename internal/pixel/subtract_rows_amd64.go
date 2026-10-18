//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

func subtract16Rows(sp unsafe.Pointer, ss int, pp unsafe.Pointer, ps int, op unsafe.Pointer, os, w, h int) bool {
	defer archsimd.ClearAVXUpperBits()
	var bits archsimd.Int16x16
	for {
		for x := 0; x < w; x += 16 {
			a := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(sp, x)))
			b := archsimd.LoadUint8x16Array((*[16]byte)(unsafe.Add(pp, x)))
			d := a.ExtendToUint16().BitsToInt16().Sub(b.ExtendToUint16().BitsToInt16())
			d.StoreArray((*[16]int16)(unsafe.Add(op, 2*x)))
			bits = bits.Or(d)
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
	q := bits.GetLo().Or(bits.GetHi()).ToBits().ReshapeToUint64s()
	return q.GetElem(0)|q.GetElem(1) != 0
}

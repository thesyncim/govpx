//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
	"unsafe"
)

// QuantizeVP8Batch quantizes consecutive blocks with shared immutable tables.
// It retains coefficient/block store order, including input/output overlaps.
// Table aliases, unsupported signed tables and short buffers decline before
// any store so the caller can use its original per-block implementation.
func QuantizeVP8Batch(coeff []int16, round, quant, dequant *[16]int16, qcoeff, dqcoeff []int16, iscan *[16]int16, eobs []uint8, count int) bool {
	if !Enabled || count <= 0 || count > min(len(coeff), len(qcoeff), len(dqcoeff))/16 || count > len(eobs) {
		return false
	}
	// Validate element counts before multiplying; uintptr byte offsets also
	// avoid signed overflow for large accepted counts.
	n := count * 16

	// A conservative span covers the shared encoder BlockQuant tables.
	// Separately allocated tables may decline, preserving the general fallback.
	overlap := func(a uintptr, an uintptr, b uintptr, bn uintptr) bool {
		return (a <= b && b-a < an) || (b < a && a-b < bn)
	}
	rptr, qptr, dptr := uintptr(unsafe.Pointer(round)), uintptr(unsafe.Pointer(quant)), uintptr(unsafe.Pointer(dequant))
	lo, hi := min(rptr, qptr, dptr), max(rptr, qptr, dptr)+32
	scan := uintptr(unsafe.Pointer(iscan))
	for k, p := range [3]uintptr{uintptr(unsafe.Pointer(unsafe.SliceData(qcoeff))), uintptr(unsafe.Pointer(unsafe.SliceData(dqcoeff))), uintptr(unsafe.Pointer(unsafe.SliceData(eobs)))} {
		size := uintptr(n) * 2
		if k == 2 {
			size = uintptr(count)
		}
		if overlap(p, size, lo, hi-lo) || overlap(p, size, scan, 32) {
			return false
		}
	}
	r0, r1 := archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Pointer(round))), archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Add(unsafe.Pointer(round), 16)))
	q0, q1 := archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Pointer(quant))), archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Add(unsafe.Pointer(quant), 16)))
	if r0.Or(r1).Or(q0).Or(q1).ReduceMin() < 0 {
		return false
	}
	d0, d1 := archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Pointer(dequant))), archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Add(unsafe.Pointer(dequant), 16)))
	i0, i1 := archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Pointer(iscan))), archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Add(unsafe.Pointer(iscan), 16)))
	z := archsimd.Int16x8{}
	cp, qp, dp := unsafe.Pointer(unsafe.SliceData(coeff)), unsafe.Pointer(unsafe.SliceData(qcoeff)), unsafe.Pointer(unsafe.SliceData(dqcoeff))
	for i := range count {
		off := uintptr(i) * 32
		c0, c1 := archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Add(cp, off))), archsimd.LoadInt16x8Array((*[8]int16)(unsafe.Add(cp, off+16)))
		t0, t1 := vp8QuantPacket(c0, r0, q0), vp8QuantPacket(c1, r1, q1)
		t0.StoreArray((*[8]int16)(unsafe.Add(qp, off)))
		t1.StoreArray((*[8]int16)(unsafe.Add(qp, off+16)))
		t0.Mul(d0).StoreArray((*[8]int16)(unsafe.Add(dp, off)))
		t1.Mul(d1).StoreArray((*[8]int16)(unsafe.Add(dp, off+16)))
		eobs[i] = uint8(i0.AndNot(t0.Equal(z).ToInt16x8()).Max(i1.AndNot(t1.Equal(z).ToInt16x8())).ReduceMax())
	}
	return true
}

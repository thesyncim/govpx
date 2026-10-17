//go:build goexperiment.simd && amd64 && !purego

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

	r := archsimd.LoadInt16x16Array(round)
	q := archsimd.LoadInt16x16Array(quant)
	rq := r.Or(q)
	// Keep this load VEX encoded while r/q YMM values are live. A literal
	// XMM zero makes Go 1.27 emit legacy MOVUPS and pay an AVX/SSE transition.
	if any16(rq.GetLo().Or(rq.GetHi()).Less(archsimd.LoadInt16x8Array(&packZero16))) {
		archsimd.ClearAVXUpperBits()
		return false
	}
	d := archsimd.LoadInt16x16Array(dequant)
	positions := archsimd.LoadInt16x16Array(iscan)
	z := archsimd.Int16x16{}
	cp, qp, dp := unsafe.Pointer(unsafe.SliceData(coeff)), unsafe.Pointer(unsafe.SliceData(qcoeff)), unsafe.Pointer(unsafe.SliceData(dqcoeff))
	for i := range count {
		off := uintptr(i) * 32
		c := archsimd.LoadInt16x16Array((*[16]int16)(unsafe.Add(cp, off)))
		magnitude := c.Abs().ToBits().Add(r.ToBits()).MulHigh(q.ToBits()).BitsToInt16()
		sign := c.ShiftAllRight(15)
		t := magnitude.Xor(sign).Sub(sign)
		t.StoreArray((*[16]int16)(unsafe.Add(qp, off)))
		t.Mul(d).StoreArray((*[16]int16)(unsafe.Add(dp, off)))
		last := positions.Masked(t.NotEqual(z))
		eobs[i] = uint8(fpReduceMax16(last.GetLo().Max(last.GetHi())))
	}
	// Return to ordinary Go/SSE with no live wide vector results.
	archsimd.ClearAVXUpperBits()
	return true
}

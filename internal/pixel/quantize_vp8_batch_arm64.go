//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"simd/archsimd"
)

// QuantizeVP8Batch quantizes consecutive blocks with shared immutable tables.
// It retains coefficient/block store order, including input/output overlaps.
// Table aliases, unsupported signed tables and short buffers decline before
// any store so the caller can use its original per-block implementation.
func QuantizeVP8Batch(coeff []int16, round, quant, dequant *[16]int16, qcoeff, dqcoeff []int16, iscan *[16]int16, eobs []uint8, count int) bool {
	if !Enabled || count <= 0 || count > min(len(coeff), len(qcoeff), len(dqcoeff))/16 || count > len(eobs) {
		return false
	}
	if &qcoeff[0] == &round[0] || &qcoeff[0] == &quant[0] || &qcoeff[0] == &dequant[0] || &qcoeff[0] == &iscan[0] ||
		&dqcoeff[0] == &round[0] || &dqcoeff[0] == &quant[0] || &dqcoeff[0] == &dequant[0] || &dqcoeff[0] == &iscan[0] {
		return false
	}
	r0, r1 := archsimd.LoadInt16x8(round[:8]), archsimd.LoadInt16x8(round[8:])
	q0, q1 := archsimd.LoadInt16x8(quant[:8]), archsimd.LoadInt16x8(quant[8:])
	if r0.Or(r1).Or(q0).Or(q1).ReduceMin() < 0 {
		return false
	}
	d0, d1 := archsimd.LoadInt16x8Array((*[8]int16)(dequant[:8])), archsimd.LoadInt16x8Array((*[8]int16)(dequant[8:16]))
	i0, i1 := archsimd.LoadInt16x8Array((*[8]int16)(iscan[:8])), archsimd.LoadInt16x8Array((*[8]int16)(iscan[8:16]))
	z := archsimd.Int16x8{}
	n := count * 16
	coeff = coeff[:n:n]
	qcoeff = qcoeff[:n:n]
	dqcoeff = dqcoeff[:n:n]
	eobs = eobs[:count:count]
	for i := range count {
		off := i * 16
		c0 := archsimd.LoadInt16x8Array((*[8]int16)(coeff[off : off+8]))
		c1 := archsimd.LoadInt16x8Array((*[8]int16)(coeff[off+8 : off+16]))
		t0, t1 := vp8QuantPacket(c0, r0, q0), vp8QuantPacket(c1, r1, q1)
		t0.StoreArray((*[8]int16)(qcoeff[off : off+8]))
		t1.StoreArray((*[8]int16)(qcoeff[off+8 : off+16]))
		t0.Mul(d0).StoreArray((*[8]int16)(dqcoeff[off : off+8]))
		t1.Mul(d1).StoreArray((*[8]int16)(dqcoeff[off+8 : off+16]))
		eobs[i] = uint8(i0.AndNot(t0.Equal(z).ToInt16x8()).Max(i1.AndNot(t1.Equal(z).ToInt16x8())).ReduceMax())
	}
	return true
}

// QuantizeVP8BatchOnly omits dequantized coefficients when the caller only
// consumes quantized coefficients and EOBs. Store order and fallback guards
// match QuantizeVP8Batch for its remaining inputs and outputs.
func QuantizeVP8BatchOnly(coeff []int16, round, quant *[16]int16, qcoeff []int16, iscan *[16]int16, eobs []uint8, count int) bool {
	if !Enabled || count <= 0 || count > min(len(coeff), len(qcoeff))/16 || count > len(eobs) {
		return false
	}
	if &qcoeff[0] == &round[0] || &qcoeff[0] == &quant[0] || &qcoeff[0] == &iscan[0] {
		return false
	}
	r0, r1 := archsimd.LoadInt16x8Array((*[8]int16)(round[:8])), archsimd.LoadInt16x8Array((*[8]int16)(round[8:16]))
	q0, q1 := archsimd.LoadInt16x8Array((*[8]int16)(quant[:8])), archsimd.LoadInt16x8Array((*[8]int16)(quant[8:16]))
	if r0.Or(r1).Or(q0).Or(q1).ReduceMin() < 0 {
		return false
	}
	i0, i1 := archsimd.LoadInt16x8Array((*[8]int16)(iscan[:8])), archsimd.LoadInt16x8Array((*[8]int16)(iscan[8:16]))
	z := archsimd.Int16x8{}
	n := count * 16
	coeff = coeff[:n:n]
	qcoeff = qcoeff[:n:n]
	eobs = eobs[:count:count]
	for i := range count {
		off := i * 16
		c0 := archsimd.LoadInt16x8Array((*[8]int16)(coeff[off : off+8]))
		c1 := archsimd.LoadInt16x8Array((*[8]int16)(coeff[off+8 : off+16]))
		t0, t1 := vp8QuantPacket(c0, r0, q0), vp8QuantPacket(c1, r1, q1)
		t0.StoreArray((*[8]int16)(qcoeff[off : off+8]))
		t1.StoreArray((*[8]int16)(qcoeff[off+8 : off+16]))
		eobs[i] = uint8(i0.AndNot(t0.Equal(z).ToInt16x8()).Max(i1.AndNot(t1.Equal(z).ToInt16x8())).ReduceMax())
	}
	return true
}

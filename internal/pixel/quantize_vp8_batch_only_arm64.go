//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

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
	r0, r1 := archsimd.LoadInt16x8(round[:8]), archsimd.LoadInt16x8(round[8:])
	q0, q1 := archsimd.LoadInt16x8(quant[:8]), archsimd.LoadInt16x8(quant[8:])
	if r0.Or(r1).Or(q0).Or(q1).ReduceMin() < 0 {
		return false
	}
	i0, i1 := archsimd.LoadInt16x8(iscan[:8]), archsimd.LoadInt16x8(iscan[8:])
	z := archsimd.Int16x8{}
	for i := range count {
		off := i * 16
		c0, c1 := archsimd.LoadInt16x8(coeff[off:]), archsimd.LoadInt16x8(coeff[off+8:])
		t0, t1 := vp8QuantPacket(c0, r0, q0), vp8QuantPacket(c1, r1, q1)
		t0.Store(qcoeff[off:])
		t1.Store(qcoeff[off+8:])
		eobs[i] = uint8(i0.AndNot(t0.Equal(z).ToInt16x8()).Max(i1.AndNot(t1.Equal(z).ToInt16x8())).ReduceMax())
	}
	return true
}

//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

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
	for _, table := range [4][]int16{round[:], quant[:], dequant[:], iscan[:]} {
		if slicesOverlap(qcoeff, table, n, 16) || slicesOverlap(dqcoeff, table, n, 16) {
			return false
		}
	}
	r0, r1 := archsimd.LoadInt16x8(round[:8]), archsimd.LoadInt16x8(round[8:])
	q0, q1 := archsimd.LoadInt16x8(quant[:8]), archsimd.LoadInt16x8(quant[8:])
	if r0.Or(r1).Or(q0).Or(q1).ReduceMin() < 0 {
		return false
	}
	d0, d1 := archsimd.LoadInt16x8(dequant[:8]), archsimd.LoadInt16x8(dequant[8:])
	i0, i1 := archsimd.LoadInt16x8(iscan[:8]), archsimd.LoadInt16x8(iscan[8:])
	z := archsimd.Int16x8{}
	for i := range count {
		off := i * 16
		c0, c1 := archsimd.LoadInt16x8(coeff[off:]), archsimd.LoadInt16x8(coeff[off+8:])
		t0, t1 := vp8QuantPacket(c0, r0, q0), vp8QuantPacket(c1, r1, q1)
		t0.Store(qcoeff[off:])
		t1.Store(qcoeff[off+8:])
		t0.Mul(d0).Store(dqcoeff[off:])
		t1.Mul(d1).Store(dqcoeff[off+8:])
		eobs[i] = uint8(i0.AndNot(t0.Equal(z).ToInt16x8()).Max(i1.AndNot(t1.Equal(z).ToInt16x8())).ReduceMax())
	}
	return true
}

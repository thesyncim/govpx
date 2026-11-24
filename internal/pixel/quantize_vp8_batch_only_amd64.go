//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// QuantizeVP8BatchOnly omits dequantized coefficients when the caller only
// consumes quantized coefficients and EOBs. Store order and fallback guards
// match QuantizeVP8Batch for its remaining inputs and outputs.
func QuantizeVP8BatchOnly(coeff []int16, round, quant *[16]int16, qcoeff []int16, iscan *[16]int16, eobs []uint8, count int) bool {
	if !Enabled || count <= 0 || count > min(len(coeff), len(qcoeff))/16 || count > len(eobs) {
		return false
	}
	// Validate element counts before multiplying; uintptr byte offsets also
	// avoid signed overflow for large accepted counts.
	n := count * 16
	for _, table := range [3][]int16{round[:], quant[:], iscan[:]} {
		if slicesOverlap(qcoeff, table, n, 16) {
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
	positions := archsimd.LoadInt16x16Array(iscan)
	z := archsimd.Int16x16{}
	for i := range count {
		off := i * 16
		c := archsimd.LoadInt16x16(coeff[off:])
		magnitude := c.Abs().ToBits().Add(r.ToBits()).MulHigh(q.ToBits()).BitsToInt16()
		sign := c.ShiftAllRight(15)
		t := magnitude.Xor(sign).Sub(sign)
		t.Store(qcoeff[off:])
		last := positions.Masked(t.NotEqual(z))
		eobs[i] = uint8(fpReduceMax16(last.GetLo().Max(last.GetHi())))
	}
	// Return to ordinary Go/SSE with no live wide vector results.
	archsimd.ClearAVXUpperBits()
	return true
}

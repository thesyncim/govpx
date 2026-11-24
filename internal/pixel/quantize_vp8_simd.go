//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import "simd/archsimd"

func vp8QuantPacket(c, r, q archsimd.Int16x8) archsimd.Int16x8 {
	// |c| <=32768 and round<=32767, so the unsigned sum fits without clipping.
	magnitude := vp8MulHighU16(c.Abs().ToBits().Add(r.ToBits()), q.ToBits()).BitsToInt16()
	return archsimd.Int16x8{}.Sub(magnitude).IfElse(c.Less(archsimd.Int16x8{}), magnitude)
}
func quantizeVP8SIMD(coeff, round, quant, dequant, qcoeff, dqcoeff, iscan *[16]int16) (int, bool) {
	r0, r1 := archsimd.LoadInt16x8(round[:]), archsimd.LoadInt16x8(round[8:])
	q0, q1 := archsimd.LoadInt16x8(quant[:]), archsimd.LoadInt16x8(quant[8:])
	if any16(r0.Or(r1).Or(q0).Or(q1).Less(archsimd.Int16x8{})) {
		return 0, false
	}
	c0, c1 := archsimd.LoadInt16x8(coeff[:]), archsimd.LoadInt16x8(coeff[8:])
	t0, t1 := vp8QuantPacket(c0, r0, q0), vp8QuantPacket(c1, r1, q1)
	t0.Store(qcoeff[:])
	t1.Store(qcoeff[8:])
	t0.Mul(archsimd.LoadInt16x8(dequant[:])).Store(dqcoeff[:])
	t1.Mul(archsimd.LoadInt16x8(dequant[8:])).Store(dqcoeff[8:])
	z := archsimd.Int16x8{}
	last := archsimd.LoadInt16x8(iscan[:]).Masked(t0.NotEqual(z)).Max(archsimd.LoadInt16x8(iscan[8:]).Masked(t1.NotEqual(z)))
	return fpReduceMax16(last), true
}

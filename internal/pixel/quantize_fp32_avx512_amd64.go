//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// All used indices are clamped to0..11; upper entries are unreachable.
var fpClassWordLUT32 = [32]uint16{0, 1, 2, 3, 3, 4, 4, 4, 4, 4, 4, 5, 5, 5, 5, 5}

// quantizeFPBlock32AVX512 requires the existing public length/table checks,
// complete disjoint windows, n256..1024 divisible by32, and real AVX512().
func quantizeFPBlock32AVX512(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	max16 := archsimd.BroadcastUint16x32(32767)
	zero := archsimd.Int16x32{}
	last := zero
	var lut archsimd.Uint16x32
	if classes != nil {
		lut = archsimd.LoadUint16x32Array(&fpClassWordLUT32)
	}
	eleven := archsimd.BroadcastInt16x32(11)
	{
		i := 0
		vr := archsimd.BroadcastInt16x32(r[1]).SetLo(archsimd.BroadcastInt16x16(r[1]).SetLo(archsimd.BroadcastInt16x8(r[1]).SetElem(0, r[0])))
		vq := archsimd.BroadcastInt16x32(q[1]).SetLo(archsimd.BroadcastInt16x16(q[1]).SetLo(archsimd.BroadcastInt16x8(q[1]).SetElem(0, q[0])))
		vd := archsimd.BroadcastInt16x32(d[1]).SetLo(archsimd.BroadcastInt16x16(d[1]).SetLo(archsimd.BroadcastInt16x8(d[1]).SetElem(0, d[0])))
		c := archsimd.LoadInt16x32(coeff[i:])
		a := c.Abs().ToBits().Add(vr.ToBits()).Min(max16).BitsToInt16()
		active := a.GreaterEqual(vd)
		if active.ToBits() == 0 {
			if qcoeff != nil {
				zero.Store(qcoeff[i:])
			}
			zero.Store(dqcoeff[i:])
			if classes != nil {
				archsimd.Uint8x32{}.Store(classes[i:])
			}
		} else {
			magnitude := a.MulHigh(vq).Masked(active)
			t := zero.Sub(magnitude).IfElse(c.Less(zero), magnitude)
			if qcoeff != nil {
				t.Store(qcoeff[i:])
			}
			t.Mul(vd).Store(dqcoeff[i:])
			last = last.Max(archsimd.LoadInt16x32(iscan[i:]).Masked(t.NotEqual(zero)))
			if classes != nil {
				lut.Permute(magnitude.Min(eleven).ToBits()).TruncToUint8().Store(classes[i:])
			}
		}
	}
	acR, acQ, acD := archsimd.BroadcastInt16x32(r[1]), archsimd.BroadcastInt16x32(q[1]), archsimd.BroadcastInt16x32(d[1])
	for i := 32; i < n; i += 32 {
		c := archsimd.LoadInt16x32(coeff[i:])
		a := c.Abs().ToBits().Add(acR.ToBits()).Min(max16).BitsToInt16()
		active := a.GreaterEqual(acD)
		if active.ToBits() == 0 {
			if qcoeff != nil {
				zero.Store(qcoeff[i:])
			}
			zero.Store(dqcoeff[i:])
			if classes != nil {
				archsimd.Uint8x32{}.Store(classes[i:])
			}
			continue
		}
		magnitude := a.MulHigh(acQ).Masked(active)
		t := zero.Sub(magnitude).IfElse(c.Less(zero), magnitude)
		if qcoeff != nil {
			t.Store(qcoeff[i:])
		}
		t.Mul(acD).Store(dqcoeff[i:])
		last = last.Max(archsimd.LoadInt16x32(iscan[i:]).Masked(t.NotEqual(zero)))
		if classes != nil {
			lut.Permute(magnitude.Min(eleven).ToBits()).TruncToUint8().Store(classes[i:])
		}
	}
	v := last.GetLo().Max(last.GetHi())
	result := fpReduceMax16(v.GetLo().Max(v.GetHi()))
	archsimd.ClearAVXUpperBits()
	return result
}

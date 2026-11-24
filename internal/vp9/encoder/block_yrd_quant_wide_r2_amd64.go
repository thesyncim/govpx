//go:build goexperiment.simd && amd64 && !purego

package encoder

import "simd/archsimd"

// Process sixteen bounded scoring coefficients per packet; no q/dq output exists.
func blockYrdQuantWideR2(c []int16, n int, r, q, d [2]int16) (int, uint32, bool) {
	ar := archsimd.BroadcastUint16x16(uint16(r[1]))
	aq, ad := archsimd.BroadcastInt16x16(q[1]), archsimd.BroadcastInt16x16(d[1])
	rr := ar.SetLo(ar.GetLo().SetElem(0, uint16(r[0])))
	qq, dd := aq.SetLo(aq.GetLo().SetElem(0, q[0])), ad.SetLo(ad.GetLo().SetElem(0, d[0]))
	max16 := archsimd.BroadcastUint16x16(32767)
	ones := archsimd.BroadcastInt16x16(1)
	rate, err := archsimd.Int32x8{}, archsimd.Int32x8{}
	for i := 0; i < n; i += 16 {
		mag := archsimd.LoadInt16x16Array((*[16]int16)(c[i : i+16])).Abs().ToBits()
		a := mag.Add(rr).Min(max16).BitsToInt16()
		t := a.MulHigh(qq)
		rate = rate.Add(t.DotProductPairs(ones))
		diff := mag.Sub(t.Mul(dd).ToBits()).BitsToInt16()
		err = err.Add(diff.DotProductPairs(diff))
		rr, qq, dd = ar, aq, ad
	}
	rt, et := blockYrdSum32(rate.ToBits().GetLo().Add(rate.ToBits().GetHi())), blockYrdSum32(err.ToBits().GetLo().Add(err.ToBits().GetHi()))
	archsimd.ClearAVXUpperBits()
	return int(rt), et, true
}

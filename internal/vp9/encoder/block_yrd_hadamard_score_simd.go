//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import "simd/archsimd"

// The BlockYrd scoring caller proves complete residual rows, disjoint scratch,
// and the bounded quantizer contract documented by blockYrdQuantScore.
func blockYrdHadamard8Score(input []int16, stride int, r, q, d [2]int16) (int, uint32) {
	v0 := archsimd.LoadInt16x8Array((*[8]int16)(input[0*stride : 0*stride+8]))
	v1 := archsimd.LoadInt16x8Array((*[8]int16)(input[1*stride : 1*stride+8]))
	v2 := archsimd.LoadInt16x8Array((*[8]int16)(input[2*stride : 2*stride+8]))
	v3 := archsimd.LoadInt16x8Array((*[8]int16)(input[3*stride : 3*stride+8]))
	v4 := archsimd.LoadInt16x8Array((*[8]int16)(input[4*stride : 4*stride+8]))
	v5 := archsimd.LoadInt16x8Array((*[8]int16)(input[5*stride : 5*stride+8]))
	v6 := archsimd.LoadInt16x8Array((*[8]int16)(input[6*stride : 6*stride+8]))
	v7 := archsimd.LoadInt16x8Array((*[8]int16)(input[7*stride : 7*stride+8]))
	{
		b0, b1 := v0.Add(v1), v0.Sub(v1)
		b2, b3 := v2.Add(v3), v2.Sub(v3)
		b4, b5 := v4.Add(v5), v4.Sub(v5)
		b6, b7 := v6.Add(v7), v6.Sub(v7)
		c0, c1, c2, c3 := b0.Add(b2), b1.Add(b3), b0.Sub(b2), b1.Sub(b3)
		c4, c5, c6, c7 := b4.Add(b6), b5.Add(b7), b4.Sub(b6), b5.Sub(b7)
		v0, v7, v3, v4, v2, v6, v1, v5 = c0.Add(c4), c1.Add(c5), c2.Add(c6), c3.Add(c7), c0.Sub(c4), c1.Sub(c5), c2.Sub(c6), c3.Sub(c7)
	}
	{
		a0, a1 := v0.InterleaveLo(v1).ToBits().ReshapeToUint32s(), v0.InterleaveHi(v1).ToBits().ReshapeToUint32s()
		a2, a3 := v2.InterleaveLo(v3).ToBits().ReshapeToUint32s(), v2.InterleaveHi(v3).ToBits().ReshapeToUint32s()
		a4, a5 := v4.InterleaveLo(v5).ToBits().ReshapeToUint32s(), v4.InterleaveHi(v5).ToBits().ReshapeToUint32s()
		a6, a7 := v6.InterleaveLo(v7).ToBits().ReshapeToUint32s(), v6.InterleaveHi(v7).ToBits().ReshapeToUint32s()
		b0, b1, b2, b3 := a0.InterleaveLo(a2).ReshapeToUint64s(), a0.InterleaveHi(a2).ReshapeToUint64s(), a1.InterleaveLo(a3).ReshapeToUint64s(), a1.InterleaveHi(a3).ReshapeToUint64s()
		b4, b5, b6, b7 := a4.InterleaveLo(a6).ReshapeToUint64s(), a4.InterleaveHi(a6).ReshapeToUint64s(), a5.InterleaveLo(a7).ReshapeToUint64s(), a5.InterleaveHi(a7).ReshapeToUint64s()
		v0, v1, v2, v3 = b0.InterleaveLo(b4).ReshapeToUint16s().BitsToInt16(), b0.InterleaveHi(b4).ReshapeToUint16s().BitsToInt16(), b1.InterleaveLo(b5).ReshapeToUint16s().BitsToInt16(), b1.InterleaveHi(b5).ReshapeToUint16s().BitsToInt16()
		v4, v5, v6, v7 = b2.InterleaveLo(b6).ReshapeToUint16s().BitsToInt16(), b2.InterleaveHi(b6).ReshapeToUint16s().BitsToInt16(), b3.InterleaveLo(b7).ReshapeToUint16s().BitsToInt16(), b3.InterleaveHi(b7).ReshapeToUint16s().BitsToInt16()
	}
	{
		b0, b1 := v0.Add(v1), v0.Sub(v1)
		b2, b3 := v2.Add(v3), v2.Sub(v3)
		b4, b5 := v4.Add(v5), v4.Sub(v5)
		b6, b7 := v6.Add(v7), v6.Sub(v7)
		c0, c1, c2, c3 := b0.Add(b2), b1.Add(b3), b0.Sub(b2), b1.Sub(b3)
		c4, c5, c6, c7 := b4.Add(b6), b5.Add(b7), b4.Sub(b6), b5.Sub(b7)
		v0, v7, v3, v4, v2, v6, v1, v5 = c0.Add(c4), c1.Add(c5), c2.Add(c6), c3.Add(c7), c0.Sub(c4), c1.Sub(c5), c2.Sub(c6), c3.Sub(c7)
	}
	ar := archsimd.BroadcastUint16x8(uint16(r[1]))
	aq := archsimd.BroadcastInt16x8(q[1])
	ad := archsimd.BroadcastInt16x8(d[1])
	rr, qq, dd := ar.SetElem(0, uint16(r[0])), aq.SetElem(0, q[0]), ad.SetElem(0, d[0])
	max16 := archsimd.BroadcastUint16x8(32767)
	err0, err1 := archsimd.Uint32x4{}, archsimd.Uint32x4{}
	if d[0] >= 16 && d[1] >= 16 {
		// When both DC and AC dequants are >= 16, each quantized token magnitude
		// is <= 4096, so 8 rows per lane sum to <= 32768 < 65535 (fits Uint16x8).
		var rate16 archsimd.Uint16x8
		{
			mag := v0.Abs().ToBits()
			a := mag.Add(rr).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, qq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(dd).ToBits()).BitsToInt16()
			err0 = err0.Add(blockYrdSquarePairs16(diff))
		}
		{
			mag := v1.Abs().ToBits()
			a := mag.Add(ar).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, aq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
			err1 = err1.Add(blockYrdSquarePairs16(diff))
		}
		{
			mag := v2.Abs().ToBits()
			a := mag.Add(ar).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, aq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
			err0 = err0.Add(blockYrdSquarePairs16(diff))
		}
		{
			mag := v3.Abs().ToBits()
			a := mag.Add(ar).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, aq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
			err1 = err1.Add(blockYrdSquarePairs16(diff))
		}
		{
			mag := v4.Abs().ToBits()
			a := mag.Add(ar).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, aq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
			err0 = err0.Add(blockYrdSquarePairs16(diff))
		}
		{
			mag := v5.Abs().ToBits()
			a := mag.Add(ar).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, aq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
			err1 = err1.Add(blockYrdSquarePairs16(diff))
		}
		{
			mag := v6.Abs().ToBits()
			a := mag.Add(ar).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, aq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
			err0 = err0.Add(blockYrdSquarePairs16(diff))
		}
		{
			mag := v7.Abs().ToBits()
			a := mag.Add(ar).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, aq)
			rate16 = rate16.Add(t.ToBits())
			diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
			err1 = err1.Add(blockYrdSquarePairs16(diff))
		}
		rate := blockYrdSumPairs16(rate16.BitsToInt16())
		return int(blockYrdSum32(rate)), blockYrdSum32(err0.Add(err1))
	}
	rate := archsimd.Uint32x4{}
	{
		mag := v0.Abs().ToBits()
		a := mag.Add(rr).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, qq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(dd).ToBits()).BitsToInt16()
		err0 = err0.Add(blockYrdSquarePairs16(diff))
	}
	{
		mag := v1.Abs().ToBits()
		a := mag.Add(ar).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, aq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
		err1 = err1.Add(blockYrdSquarePairs16(diff))
	}
	{
		mag := v2.Abs().ToBits()
		a := mag.Add(ar).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, aq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
		err0 = err0.Add(blockYrdSquarePairs16(diff))
	}
	{
		mag := v3.Abs().ToBits()
		a := mag.Add(ar).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, aq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
		err1 = err1.Add(blockYrdSquarePairs16(diff))
	}
	{
		mag := v4.Abs().ToBits()
		a := mag.Add(ar).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, aq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
		err0 = err0.Add(blockYrdSquarePairs16(diff))
	}
	{
		mag := v5.Abs().ToBits()
		a := mag.Add(ar).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, aq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
		err1 = err1.Add(blockYrdSquarePairs16(diff))
	}
	{
		mag := v6.Abs().ToBits()
		a := mag.Add(ar).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, aq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
		err0 = err0.Add(blockYrdSquarePairs16(diff))
	}
	{
		mag := v7.Abs().ToBits()
		a := mag.Add(ar).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, aq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(ad).ToBits()).BitsToInt16()
		err1 = err1.Add(blockYrdSquarePairs16(diff))
	}
	return int(blockYrdSum32(rate)), blockYrdSum32(err0.Add(err1))
}

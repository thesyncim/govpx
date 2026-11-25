//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"simd/archsimd"
)

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

// blockYrdScore specializes the libvpx v1.16.0 vp9_pickmode.c:781-853
// transform/quantize/SATD/error walk to its aggregate-only consumer. Neither
// qcoeff/dqcoeff nor a scan position escapes BlockYrd. A zero sum(abs(q)) is
// exactly EOB==0; the DC-only EOB==1 rate is the same sum.
func blockYrdScore(src []int16, bw, bh int, tx common.TxSize,
	r, q, d [2]int16, sse uint64, coeff []int16, nonzero bool,
) (BlockYrdResult, bool) {
	if !pixel.Enabled || tx < common.Tx4x4 || tx > common.Tx16x16 ||
		d[0] < 3 || d[0] > 2048 || d[1] < 3 || d[1] > 2048 {
		return BlockYrdResult{}, false
	}
	n := 4 << uint(tx)
	units := (bw / n) * (bh / n)
	res := BlockYrdResult{Valid: true, Skippable: true}
	known := sse < BlockYrdUnknownSSE
	if known {
		res.SSE = int64(sse << 4)
	}
	// Subtraction already reports this fact. Zero Hadamard input produces
	// only zero coefficients, and these quantizer rounds are below dequant.
	if !nonzero {
		if known {
			res.Dist = res.SSE
		} else {
			res.Rate = units << VP9ProbCostShift
		}
		return res, true
	}
	var rate int
	var dist int64
	for y := 0; y < bh; y += n {
		for x := 0; x < bw; x += n {
			input := src[y*bw+x:]
			var rtx int
			var etx uint32
			uniform := false
			if n == 4 {
				ForwardDCT4x4Into(input, bw, coeff)
				rtx, etx = blockYrdQuantScore(coeff, 16, r, q, d)
				uniform = true
			} else {
				rtx, etx, uniform = blockYrdUniformScore(input, bw, n, r, q, d)
			}
			if !uniform {
				if n == 8 {
					rtx, etx = blockYrdHadamard8Score(input, bw, r, q, d)
				} else {
					hadamardBlockYrd(input, bw, coeff, n)
					rtx, etx = blockYrdQuantScore(coeff, n*n, r, q, d)
				}
			}
			rate += rtx
			dist += int64(etx >> 2)
		}
	}
	res.Skippable = rate == 0
	if res.Skippable && known {
		res.Dist = res.SSE
		return res, true
	}
	res.Rate = rate<<(2+VP9ProbCostShift) + units<<VP9ProbCostShift
	res.Dist = dist
	return res, true
}

// The caller supplies n=16/64/256 complete coefficients and the exact BlockYrd
// tables r=floor(factor*d/128), q=floor(65536/d), with 3<=d<=2048.
// The usual threshold mask is redundant here: a<d implies
// floor(a*floor(65536/d)/65536)==0. Thus q>=0 and qabs*d<=32767: dequantization cannot wrap. Sign restoration
// can be omitted because both abs(q) and (coeff-dq)^2 are sign invariant.
// For a=abs(coeff), clipped=min(a+r,32767), the reciprocal truncation gives
// -r<=a-qabs*d<1+1.5*d<=3073. Consequently every squared error fits int32,
// each four-lane accumulator is below 2^31, and the 256-coefficient total
// is below 2^32. Larger or unusual tables retain the general path.
func blockYrdQuantScore(coeff []int16, n int, r, q, d [2]int16) (int, uint32) {
	if n == 256 {
		if rate, err, ok := blockYrdQuantWideR2(coeff, n, r, q, d); ok {
			return rate, err
		}
	}
	// For d>=16, each qabs<=2047. At most 32 packets contribute to
	// a lane, so 32*2047=65504 fits uint16 without per-packet widening.
	if d[0] >= 16 && d[1] >= 16 {
		acR := archsimd.BroadcastUint16x8(uint16(r[1]))
		acQ, acD := archsimd.BroadcastInt16x8(q[1]), archsimd.BroadcastInt16x8(d[1])
		vr := acR.SetElem(0, uint16(r[0]))
		vq, vd := acQ.SetElem(0, q[0]), acD.SetElem(0, d[0])
		max16 := archsimd.BroadcastUint16x8(32767)
		rate, err := archsimd.Uint16x8{}, archsimd.Uint32x4{}
		for i := 0; i < n; i += 8 {
			c := archsimd.LoadInt16x8Array((*[8]int16)(coeff[i : i+8]))
			mag := c.Abs().ToBits()
			a := mag.Add(vr).Min(max16).BitsToInt16()
			t := blockYrdMulHigh16(a, vq)
			rate = rate.Add(t.ToBits())
			diff := mag.Sub(t.Mul(vd).ToBits()).BitsToInt16()
			err = err.Add(blockYrdSquarePairs16(diff))
			vr, vq, vd = acR, acQ, acD
		}
		return int(blockYrdSumUnsigned16(rate)), blockYrdSum32(err)
	}

	acR := archsimd.BroadcastUint16x8(uint16(r[1]))
	acQ, acD := archsimd.BroadcastInt16x8(q[1]), archsimd.BroadcastInt16x8(d[1])
	vr := acR.SetElem(0, uint16(r[0]))
	vq, vd := acQ.SetElem(0, q[0]), acD.SetElem(0, d[0])
	max16 := archsimd.BroadcastUint16x8(32767)
	rate, err := archsimd.Uint32x4{}, archsimd.Uint32x4{}
	for i := 0; i < n; i += 8 {
		c := archsimd.LoadInt16x8Array((*[8]int16)(coeff[i : i+8]))
		mag := c.Abs().ToBits()
		a := mag.Add(vr).Min(max16).BitsToInt16()
		t := blockYrdMulHigh16(a, vq)
		rate = rate.Add(blockYrdSumPairs16(t))
		diff := mag.Sub(t.Mul(vd).ToBits()).BitsToInt16()
		err = err.Add(blockYrdSquarePairs16(diff))
		vr, vq, vd = acR, acQ, acD
	}
	return int(blockYrdSum32(rate)), blockYrdSum32(err)
}

// The BlockYrd scoring caller proves complete residual rows, disjoint scratch,
// and the bounded quantizer contract documented by blockYrdQuantScore.
func blockYrdUniformScore(input []int16, stride, n int, r, q, d [2]int16) (int, uint32, bool) {
	v := input[0]
	same := archsimd.BroadcastInt16x8(v)
	for y := 0; y < n; y++ {
		row := input[y*stride:]
		for x := 0; x < n; x += 8 {
			c := archsimd.LoadInt16x8Array((*[8]int16)(row[x : x+8]))
			bits := c.ToBits().Xor(same.ToBits()).ReshapeToUint64s()
			if bits.GetElem(0)|bits.GetElem(1) != 0 {
				return 0, 0, false
			}
		}
	}
	dc := int(v) * 64
	if n == 16 {
		dc *= 2
	}
	if dc < 0 {
		dc = -dc
	}
	a := min(dc+int(r[0]), 32767)
	rate := a * int(q[0]) >> 16
	diff := dc - rate*int(d[0])
	return rate, uint32(diff * diff), true
}

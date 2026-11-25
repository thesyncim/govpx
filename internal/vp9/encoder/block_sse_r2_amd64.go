//go:build goexperiment.simd && amd64 && !purego

package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp9/common"
	"simd/archsimd"
)

// The consumer needs only SSE, so avoid the variance kernel's signed sum.
func blockSSEOnlyR2(src []byte, so, ss int, ref []byte, ro, rs, w, h int) (uint64, bool) {
	if !pixel.Enabled || (w != 8 && w != 32 && w != 64) || h <= 0 || h > 64 || so < 0 || ro < 0 || so > len(src) || ro > len(ref) || ss < w || rs < w {
		return 0, false
	}
	src, ref = src[so:], ref[ro:]
	if len(src) < w || len(ref) < w || h-1 > (len(src)-w)/ss || h-1 > (len(ref)-w)/rs {
		return 0, false
	}
	switch w {
	case 8:
		return uint64(pixel.SSE8(src, ss, ref, rs, h)), true
	case 16:
		return uint64(pixel.SSE16(src, ss, ref, rs, h)), true
	}
	return pixel.SSEPlane16(src, ss, ref, rs, w, h)
}

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

func blockYrdMulHigh16(a, b archsimd.Int16x8) archsimd.Int16x8 { return a.MulHigh(b) }

func blockYrdSumPairs16(v archsimd.Int16x8) archsimd.Uint32x4 {
	return v.DotProductPairs(archsimd.BroadcastInt16x8(1)).ToBits()
}

func blockYrdSquarePairs16(v archsimd.Int16x8) archsimd.Uint32x4 {
	return v.DotProductPairs(v).ToBits()
}

func blockYrdSum32(v archsimd.Uint32x4) uint32 {
	v = v.Add(v.PermuteScalars(2, 3, 0, 1))
	return v.Add(v.PermuteScalars(1, 0, 3, 2)).GetElem(0)
}

func blockYrdSumUnsigned16(v archsimd.Uint16x8) uint32 {
	hi := v.ReshapeToUint32s().PermuteScalars(2, 3, 0, 1).ReshapeToUint16s()
	return blockYrdSum32(v.ExtendLo4ToUint32().Add(hi.ExtendLo4ToUint32()))
}

// A uniform residual has only one Hadamard coefficient. Detect it directly
// before storing residuals or rechecking the same fact for each transform.
func blockYrdUniformPixelsR2(src []byte, ss, so int, dst []byte, ds, do, bw, bh int, tx common.TxSize, d [2]int16, sse uint64, scratch []int16) (BlockYrdResult, bool) {
	if (uint(ss)|uint(ds))>>30 != 0 || so < 0 || do < 0 || so > len(src) || do > len(dst) || !pixel.Enabled || (tx != common.Tx8x8 && tx != common.Tx16x16) || bw < 16 || bw > 64 || bh <= 0 || bh > 64 || bw%16 != 0 || bh%(4<<uint(tx)) != 0 || len(scratch) < 4*bw*bh || d[0] < 3 || d[0] > 2048 || d[1] < 3 || d[1] > 2048 {
		return BlockYrdResult{}, false
	}
	if len(src)-so < bw || len(dst)-do < bw || bh-1 > (len(src)-so-bw)/ss || bh-1 > (len(dst)-do-bw)/ds {
		return BlockYrdResult{}, false
	}
	delta := int16(src[so]) - int16(dst[do])
	// The existing subtract kernel already handles zero residuals cheaply.
	if delta == 0 {
		return BlockYrdResult{}, false
	}
	vd := archsimd.BroadcastInt16x16(delta)
	for y := 0; y < bh; y++ {
		srow := src[so+y*ss:]
		drow := dst[do+y*ds:]
		for x := 0; x < bw; x += 16 {
			a := archsimd.LoadUint8x16Array((*[16]byte)(srow[x : x+16])).ExtendToUint16().BitsToInt16()
			b := archsimd.LoadUint8x16Array((*[16]byte)(drow[x : x+16])).ExtendToUint16().BitsToInt16()
			if !a.Sub(b).NotEqual(vd).ToInt16x16().IsZero() {
				archsimd.ClearAVXUpperBits()
				return BlockYrdResult{}, false
			}
		}
	}
	archsimd.ClearAVXUpperBits()
	n := 4 << uint(tx)
	dc := int(delta) * 64
	if n == 16 {
		dc *= 2
	}
	if dc < 0 {
		dc = -dc
	}
	r := 48 * int(d[0]) >> 7
	q := 65536 / int(d[0])
	level := min(dc+r, 32767) * q >> 16
	diff := dc - level*int(d[0])
	units := bw * bh / (n * n)
	res := BlockYrdResult{Valid: true, Skippable: level == 0}
	if sse < BlockYrdUnknownSSE {
		res.SSE = int64(sse << 4)
		if level == 0 {
			res.Dist = res.SSE
			return res, true
		}
	}
	res.Rate = (level*units)<<(2+VP9ProbCostShift) + units<<VP9ProbCostShift
	res.Dist = int64((diff*diff)>>2) * int64(units)
	return res, true
}

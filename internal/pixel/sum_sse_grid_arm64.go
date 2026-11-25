//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// SumSSE8x8Grid computes per-8x8-block (sse, sum) across a w x h region
// (w a positive multiple of 16, h a positive multiple of 8), processing two
// horizontally adjacent 8x8 blocks per 16-byte NEON load.
func SumSSE8x8Grid(src []byte, ss int, ref []byte, rs, w, h int, sseOut []uint32, sumOut []int) (uint32, int, bool) {
	if !Enabled || w <= 0 || h <= 0 || w&15 != 0 || h&7 != 0 || ss < 0 || rs < 0 {
		return 0, 0, false
	}
	blocks := (w >> 3) * (h >> 3)
	if len(sseOut) < blocks || len(sumOut) < blocks {
		return 0, 0, false
	}
	if ss <= 1<<20 && rs <= 1<<20 && w <= 64 && h <= 64 {
		if len(src) < (h-1)*ss+w || len(ref) < (h-1)*rs+w {
			return 0, 0, false
		}
	} else if len(src) < w || len(ref) < w || (h > 1 && (ss > (len(src)-w)/(h-1) || rs > (len(ref)-w)/(h-1))) {
		return 0, 0, false
	}
	_ = src[(h-1)*ss+w-1]
	_ = ref[(h-1)*rs+w-1]
	_ = sseOut[blocks-1]
	_ = sumOut[blocks-1]

	var totalSSE uint32
	totalSum := 0
	k := 0
	for y := 0; y < h; y += 8 {
		rowS := y * ss
		rowR := y * rs
		for x := 0; x < w; x += 16 {
			var s0, s1 archsimd.Int32x4
			var sum0, sum1 archsimd.Int16x8
			so := rowS + x
			ro := rowR + x
			for range 8 {
				a := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				b := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
				lo := a.ExtendLo8ToUint16().BitsToInt16().Sub(b.ExtendLo8ToUint16().BitsToInt16())
				hi := widenHi8(a).BitsToInt16().Sub(widenHi8(b).BitsToInt16())
				sum0 = sum0.Add(lo)
				sum1 = sum1.Add(hi)
				s0 = s0.Add(square8(lo))
				s1 = s1.Add(square8(hi))
				so += ss
				ro += rs
			}
			sse0 := uint32(sum32(s0))
			sse1 := uint32(sum32(s1))
			sm0 := int(sum0.ReduceSum())
			sm1 := int(sum1.ReduceSum())
			sseOut[k] = sse0
			sumOut[k] = sm0
			sseOut[k+1] = sse1
			sumOut[k+1] = sm1
			totalSSE += sse0 + sse1
			totalSum += sm0 + sm1
			k += 2
		}
	}
	return totalSSE, totalSum, true
}

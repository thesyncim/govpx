//go:build !goexperiment.simd || !arm64 || purego

package pixel

// SumSSE8x8Grid computes per-8x8-block (sse, sum) across a w x h region
// (w a positive multiple of 16, h a positive multiple of 8).
func SumSSE8x8Grid(src []byte, ss int, ref []byte, rs, w, h int, sseOut []uint32, sumOut []int) (uint32, int, bool) {
	if w <= 0 || h <= 0 || w&15 != 0 || h&7 != 0 || ss < 0 || rs < 0 {
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
		for x := 0; x < w; x += 8 {
			var sum0, sum1, sse0, sse1, sse2, sse3 int
			so := rowS + x
			ro := rowR + x
			for range 8 {
				s := (*[8]byte)(src[so : so+8])
				r := (*[8]byte)(ref[ro : ro+8])
				d0 := int(s[0]) - int(r[0])
				d1 := int(s[1]) - int(r[1])
				d2 := int(s[2]) - int(r[2])
				d3 := int(s[3]) - int(r[3])
				sum0 += d0 + d1 + d2 + d3
				sse0 += d0 * d0
				sse1 += d1 * d1
				sse2 += d2 * d2
				sse3 += d3 * d3
				d4 := int(s[4]) - int(r[4])
				d5 := int(s[5]) - int(r[5])
				d6 := int(s[6]) - int(r[6])
				d7 := int(s[7]) - int(r[7])
				sum1 += d4 + d5 + d6 + d7
				sse0 += d4 * d4
				sse1 += d5 * d5
				sse2 += d6 * d6
				sse3 += d7 * d7
				so += ss
				ro += rs
			}
			bSSE := uint32(sse0 + sse1 + sse2 + sse3)
			bSum := sum0 + sum1
			sseOut[k] = bSSE
			sumOut[k] = bSum
			totalSSE += bSSE
			totalSum += bSum
			k++
		}
	}
	return totalSSE, totalSum, true
}

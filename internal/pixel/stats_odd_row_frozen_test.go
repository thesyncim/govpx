//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import "simd/archsimd"

func frozenOddRowStats(src []byte, ss int, ref []byte, rs, w, h int) (int, uint32) {
	if !Enabled {
		return StatsScalar(src, ss, ref, rs, w, h)
	}

	if (w == 4 && h%4 == 0 || w == 8 || w >= 16 && w%16 == 0) && w <= 64 {
		if h <= 4096 {
			sse, sum := frozenOddRowpixelStatsSIMD(src, ss, ref, rs, w, h)
			return int(sum), sse
		}
		return frozenOddRowstatsTallSIMD(src, ss, ref, rs, w, h)
	}

	var sums, squares archsimd.Int32x4
	tailSum := 0
	var tailSSE uint32
	for y := range h {
		a, b := src[y*ss:], ref[y*rs:]
		x := 0
		for ; x+8 <= w; x += 8 {
			d := widen8(load8(a[x:])).Sub(widen8(load8(b[x:])))
			sums = sums.Add(sumDiff16(d))
			squares = squares.Add(square8(d))
		}
		if x+4 <= w {
			d := LoadPixels4(a[x:]).Sub(LoadPixels4(b[x:]))
			sums = sums.Add(d)
			squares = squares.Add(d.Mul(d))
			x += 4
		}
		for ; x < w; x++ {
			d := int(a[x]) - int(b[x])
			tailSum += d
			tailSSE += uint32(d * d)
		}
	}
	return int(sum32(sums)) + tailSum, uint32(sum32(squares)) + tailSSE
}
func frozenOddRowstatsTallSIMD(src []byte, ss int, ref []byte, rs, w, h int) (int, uint32) {
	var totalSum int
	var totalSSE uint32
	for h > 4096 {
		sse, sum := frozenOddRowpixelStatsSIMD(src, ss, ref, rs, w, 4096)
		totalSum += int(sum)
		totalSSE += sse
		src, ref = src[4096*ss:], ref[4096*rs:]
		h -= 4096
	}
	sse, sum := frozenOddRowpixelStatsSIMD(src, ss, ref, rs, w, h)
	return totalSum + int(sum), totalSSE + sse
}
func frozenOddRowpixelStatsSIMD(src []byte, srcStride int, ref []byte, refStride int, w, h int) (sse uint32, sum int32) {
	switch {
	case narrowPixelStats16 && w == 16 && h > 0 && h <= 64:
		return pixelStats16Narrow(src, srcStride, ref, refStride, h)
	case w == 4 && h%4 == 0:
		return pixelStatsQuads4(src, srcStride, ref, refStride, h)
	case w == 8:
		return frozenOddRowpixelStatsPairs8(src, srcStride, ref, refStride, h)
	case w >= 16 && w&15 == 0:
		return pixelStatsRows16(src, srcStride, ref, refStride, w, h)
	}
	return pixelStatsPureGo(src, srcStride, ref, refStride, w, h)
}

// pixelStatsPureGo is govpx's glue for the extracted goav1 fallback contract.
func frozenOddRowpixelStatsPairs8(src []byte, srcStride int, ref []byte, refStride int, h int) (sse uint32, sum int32) {
	sseAcc := archsimd.BroadcastInt32x4(0)
	sumAcc := archsimd.BroadcastInt32x4(0)
	r := 0
	for ; r+2 <= h; r += 2 {
		sseAcc, sumAcc = pixelDiffAcc(sseAcc, sumAcc, packRows8(src, r*srcStride, srcStride), packRows8(ref, r*refStride, refStride))
	}
	total, signed := reducePixelAcc(sseAcc, sumAcc)
	if r < h {
		t, sg := pixelStatsPureGo(src[r*srcStride:], srcStride, ref[r*refStride:], refStride, 8, 1)
		total += pixelStatsReduction(t)
		signed += pixelStatsReduction(sg)
	}
	return uint32(total), int32(signed)
}

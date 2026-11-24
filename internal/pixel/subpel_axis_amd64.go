//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// The public dispatcher proves the complete bilinear window and bounded
// positive geometry before these exact read-only loads.
func subpelStatsAxisHorizontal16(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	if h >= 32 && h <= 64 {
		if f == [2]int16{64, 64} {
			return subpelStatsAxisHorizontal16HalfR2(src, ss, ref, rs, w, h, f)
		}
		if f[0] >= 0 && f[1] >= 0 && int(f[0])+int(f[1]) == 128 {
			return subpelStatsAxisHorizontal16SumR2(src, ss, ref, rs, w, h, f)
		}
	}
	defer archsimd.ClearAVXUpperBits()
	f0, f1, round, ones := archsimd.BroadcastInt16x16(f[0]), archsimd.BroadcastInt16x16(f[1]), archsimd.BroadcastInt16x16(64), archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		so, ro := x, x
		for range h {
			a := archsimd.LoadUint8x16(src[so:])
			b := archsimd.LoadUint8x16(src[so+1:])
			r := archsimd.LoadUint8x16(ref[ro:])
			v := a.ExtendToUint16().BitsToInt16().Mul(f0).Add(b.ExtendToUint16().BitsToInt16().Mul(f1)).Add(round).ShiftAllRight(7)
			d := v.Sub(r.ExtendToUint16().BitsToInt16())
			sum = sum.Add(d.DotProductPairs(ones))
			sse = sse.Add(d.DotProductPairs(d))
			so += ss
			ro += rs
		}
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}

// Keep the rolling row widened: a byte-vector phi can lower to a legacy SSE
// register copy inside the AVX2 loop and cause transitions on native x64.
func subpelStatsAxisVertical16(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	if h >= 32 && h <= 64 {
		if f == [2]int16{64, 64} {
			return subpelStatsAxisVertical16HalfR2(src, ss, ref, rs, w, h, f)
		}
		if f[0] >= 0 && f[1] >= 0 && int(f[0])+int(f[1]) == 128 {
			return subpelStatsAxisVertical16SumR2(src, ss, ref, rs, w, h, f)
		}
	}
	defer archsimd.ClearAVXUpperBits()
	f0, f1, round, ones := archsimd.BroadcastInt16x16(f[0]), archsimd.BroadcastInt16x16(f[1]), archsimd.BroadcastInt16x16(64), archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		so, ro := x, x
		a := archsimd.LoadUint8x16(src[x:]).ExtendToUint16().BitsToInt16()
		so += ss
		for range h {
			b := archsimd.LoadUint8x16(src[so:]).ExtendToUint16().BitsToInt16()
			r := archsimd.LoadUint8x16(ref[ro:])
			v := a.Mul(f0).Add(b.Mul(f1)).Add(round).ShiftAllRight(7)
			d := v.Sub(r.ExtendToUint16().BitsToInt16())
			sum = sum.Add(d.DotProductPairs(ones))
			sse = sse.Add(d.DotProductPairs(d))
			a = b
			so += ss
			ro += rs
		}
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}

func subpelStatsAxisHorizontal16HalfR2(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	defer archsimd.ClearAVXUpperBits()
	ones := archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		so, ro := x, x
		for range h {
			a := archsimd.LoadUint8x16(src[so:])
			b := archsimd.LoadUint8x16(src[so+1:])
			r := archsimd.LoadUint8x16(ref[ro:])
			v := a.Average(b).ExtendToUint16().BitsToInt16()
			d := v.Sub(r.ExtendToUint16().BitsToInt16())
			sum = sum.Add(d.DotProductPairs(ones))
			sse = sse.Add(d.DotProductPairs(d))
			so += ss
			ro += rs
		}
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}
func subpelStatsAxisVertical16HalfR2(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	defer archsimd.ClearAVXUpperBits()
	ones := archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		so, ro := x, x
		a := archsimd.LoadUint8x16(src[x:]).ExtendToUint16().BitsToInt16()
		so += ss
		for range h {
			b := archsimd.LoadUint8x16(src[so:]).ExtendToUint16().BitsToInt16()
			r := archsimd.LoadUint8x16(ref[ro:])
			v := a.ToBits().Average(b.ToBits()).BitsToInt16()
			d := v.Sub(r.ExtendToUint16().BitsToInt16())
			sum = sum.Add(d.DotProductPairs(ones))
			sse = sse.Add(d.DotProductPairs(d))
			a = b
			so += ss
			ro += rs
		}
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}

func subpelStatsAxisHorizontal16SumR2(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	defer archsimd.ClearAVXUpperBits()
	f0, f1, round, ones := archsimd.BroadcastInt16x16(f[0]), archsimd.BroadcastInt16x16(f[1]), archsimd.BroadcastInt16x16(64), archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		var strip archsimd.Int16x16
		so, ro := x, x
		for range h {
			a := archsimd.LoadUint8x16(src[so:])
			b := archsimd.LoadUint8x16(src[so+1:])
			r := archsimd.LoadUint8x16(ref[ro:])
			v := a.ExtendToUint16().BitsToInt16().Mul(f0).Add(b.ExtendToUint16().BitsToInt16().Mul(f1)).Add(round).ShiftAllRight(7)
			d := v.Sub(r.ExtendToUint16().BitsToInt16())
			strip = strip.Add(d)
			sse = sse.Add(d.DotProductPairs(d))
			so += ss
			ro += rs
		}
		sum = sum.Add(strip.DotProductPairs(ones))
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}
func subpelStatsAxisVertical16SumR2(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	defer archsimd.ClearAVXUpperBits()
	f0, f1, round, ones := archsimd.BroadcastInt16x16(f[0]), archsimd.BroadcastInt16x16(f[1]), archsimd.BroadcastInt16x16(64), archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		var strip archsimd.Int16x16
		so, ro := x, x
		a := archsimd.LoadUint8x16(src[x:]).ExtendToUint16().BitsToInt16()
		so += ss
		for range h {
			b := archsimd.LoadUint8x16(src[so:]).ExtendToUint16().BitsToInt16()
			r := archsimd.LoadUint8x16(ref[ro:])
			v := a.Mul(f0).Add(b.Mul(f1)).Add(round).ShiftAllRight(7)
			d := v.Sub(r.ExtendToUint16().BitsToInt16())
			strip = strip.Add(d)
			sse = sse.Add(d.DotProductPairs(d))
			a = b
			so += ss
			ro += rs
		}
		sum = sum.Add(strip.DotProductPairs(ones))
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}

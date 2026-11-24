//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

// Sixteen bilinear pixels occupy one AVX2 packet; pairwise dot products
// accumulate signed differences and squared error without narrowing.
func subpelStats16(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (int, uint32) {
	if h >= 32 && h <= 64 {
		if fx == [2]int16{64, 64} && fy == [2]int16{64, 64} {
			return subpelStats16HalfR2(src, ss, ref, rs, w, h, fx, fy)
		}
		if fx[0] >= 0 && fx[1] >= 0 && int(fx[0])+int(fx[1]) == 128 && fy[0] >= 0 && fy[1] >= 0 && int(fy[0])+int(fy[1]) == 128 {
			return subpelStats16SumR2(src, ss, ref, rs, w, h, fx, fy)
		}
	}
	return subpelStats16Slices(src, ss, ref, rs, w, h, fx, fy)
}

// subpelStats16Slices preserves the original checked-slice behavior for geometry
// outside the pointer proof. Arithmetic remains identical to the selected path.
func subpelStats16Slices(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (int, uint32) {
	_ = src[h*ss+w]
	_ = ref[(h-1)*rs+w-1]
	defer archsimd.ClearAVXUpperBits()
	x0, x1, y0, y1 := archsimd.BroadcastInt16x16(fx[0]), archsimd.BroadcastInt16x16(fx[1]), archsimd.BroadcastInt16x16(fy[0]), archsimd.BroadcastInt16x16(fy[1])
	round, ones := archsimd.BroadcastInt16x16(64), archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		a := archsimd.LoadUint8x16(src[x:]).ExtendToUint16().BitsToInt16().Mul(x0).Add(archsimd.LoadUint8x16(src[x+1:]).ExtendToUint16().BitsToInt16().Mul(x1)).Add(round).ShiftAllRight(7)
		so, ro := ss+x, x
		for range h {
			b := archsimd.LoadUint8x16(src[so:]).ExtendToUint16().BitsToInt16().Mul(x0).Add(archsimd.LoadUint8x16(src[so+1:]).ExtendToUint16().BitsToInt16().Mul(x1)).Add(round).ShiftAllRight(7)
			v := a.Mul(y0).Add(b.Mul(y1)).Add(round).ShiftAllRight(7)
			a = b
			d := v.Sub(archsimd.LoadUint8x16(ref[ro:]).ExtendToUint16().BitsToInt16())
			sum = sum.Add(d.DotProductPairs(ones))
			sse = sse.Add(d.DotProductPairs(d))
			so += ss
			ro += rs
		}
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}

func subpelStats16HalfR2(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (int, uint32) {
	if (uint(w)|uint(h)|uint(ss)|uint(rs))>>30 != 0 {
		return subpelStats16Slices(src, ss, ref, rs, w, h, fx, fy)
	}
	_ = src[h*ss+w]
	_ = ref[(h-1)*rs+w-1]
	defer archsimd.ClearAVXUpperBits()
	ones := archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		a := archsimd.LoadUint8x16(src[x:]).Average(archsimd.LoadUint8x16(src[x+1:])).ExtendToUint16().BitsToInt16()
		so, ro := ss+x, x
		for range h {
			b := archsimd.LoadUint8x16(src[so:]).Average(archsimd.LoadUint8x16(src[so+1:])).ExtendToUint16().BitsToInt16()
			v := a.ToBits().Average(b.ToBits()).BitsToInt16()
			a = b
			d := v.Sub(archsimd.LoadUint8x16(ref[ro:]).ExtendToUint16().BitsToInt16())
			sum = sum.Add(d.DotProductPairs(ones))
			sse = sse.Add(d.DotProductPairs(d))
			so += ss
			ro += rs
		}
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}

func subpelStats16SumR2(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (int, uint32) {
	if (uint(w)|uint(h)|uint(ss)|uint(rs))>>30 != 0 {
		return subpelStats16Slices(src, ss, ref, rs, w, h, fx, fy)
	}
	_ = src[h*ss+w]
	_ = ref[(h-1)*rs+w-1]
	defer archsimd.ClearAVXUpperBits()
	x0, x1, y0, y1 := archsimd.BroadcastInt16x16(fx[0]), archsimd.BroadcastInt16x16(fx[1]), archsimd.BroadcastInt16x16(fy[0]), archsimd.BroadcastInt16x16(fy[1])
	round, ones := archsimd.BroadcastInt16x16(64), archsimd.BroadcastInt16x16(1)
	var sum, sse archsimd.Int32x8
	for x := 0; x < w; x += 16 {
		var strip archsimd.Int16x16
		a := archsimd.LoadUint8x16(src[x:]).ExtendToUint16().BitsToInt16().Mul(x0).Add(archsimd.LoadUint8x16(src[x+1:]).ExtendToUint16().BitsToInt16().Mul(x1)).Add(round).ShiftAllRight(7)
		so, ro := ss+x, x
		for range h {
			b := archsimd.LoadUint8x16(src[so:]).ExtendToUint16().BitsToInt16().Mul(x0).Add(archsimd.LoadUint8x16(src[so+1:]).ExtendToUint16().BitsToInt16().Mul(x1)).Add(round).ShiftAllRight(7)
			v := a.Mul(y0).Add(b.Mul(y1)).Add(round).ShiftAllRight(7)
			a = b
			d := v.Sub(archsimd.LoadUint8x16(ref[ro:]).ExtendToUint16().BitsToInt16())
			strip = strip.Add(d)
			sse = sse.Add(d.DotProductPairs(d))
			so += ss
			ro += rs
		}
		sum = sum.Add(strip.DotProductPairs(ones))
	}
	return int(sum32(sum.GetLo().Add(sum.GetHi()))), uint32(sum32(sse.GetLo().Add(sse.GetHi())))
}

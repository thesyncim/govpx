//go:build goexperiment.simd && amd64 && !purego

package pixel

import "simd/archsimd"

func filterLinearFast(src []byte, ss int, dst []byte, ds, w, h, step int, t0, t1 uint8) bool {
	return false
}

// Sixteen output pixels per AVX2 register. The same negative-first proof as
// filterNarrow8 permits exact int16 arithmetic; byte stores use AVX2-safe
// 128-bit packing rather than AVX-512 narrowing operations.
func filterWide8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) bool {
	if len(taps) != 8 || w <= 0 || w%16 != 0 || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	mode := narrow8Mode(taps)
	if mode == 0 {
		return false
	}
	defer archsimd.ClearAVXUpperBits()
	_ = src[(h-1)*ss+w-1+7*step]
	_ = dst[(h-1)*ds+w-1]
	c0 := archsimd.BroadcastInt16x16(taps[0])
	c1 := archsimd.BroadcastInt16x16(taps[1])
	c2 := archsimd.BroadcastInt16x16(taps[2])
	c3 := archsimd.BroadcastInt16x16(taps[3])
	c4 := archsimd.BroadcastInt16x16(taps[4])
	c5 := archsimd.BroadcastInt16x16(taps[5])
	c6 := archsimd.BroadcastInt16x16(taps[6])
	c7 := archsimd.BroadcastInt16x16(taps[7])
	bias := archsimd.BroadcastInt16x16(64)
	for y := range h {
		for x := 0; x < w; x += 16 {
			p := src[y*ss+x:]
			acc := bias
			if mode == 1 {
				acc = acc.Add(archsimd.LoadUint8x16(p[0*step:]).ExtendToUint16().BitsToInt16().Mul(c0))
				acc = acc.Add(archsimd.LoadUint8x16(p[2*step:]).ExtendToUint16().BitsToInt16().Mul(c2))
				acc = acc.Add(archsimd.LoadUint8x16(p[5*step:]).ExtendToUint16().BitsToInt16().Mul(c5))
				acc = acc.Add(archsimd.LoadUint8x16(p[7*step:]).ExtendToUint16().BitsToInt16().Mul(c7))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[1*step:]).ExtendToUint16().BitsToInt16().Mul(c1))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[3*step:]).ExtendToUint16().BitsToInt16().Mul(c3))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[4*step:]).ExtendToUint16().BitsToInt16().Mul(c4))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[6*step:]).ExtendToUint16().BitsToInt16().Mul(c6))
			} else {
				acc = acc.Add(archsimd.LoadUint8x16(p[0*step:]).ExtendToUint16().BitsToInt16().Mul(c0))
				acc = acc.Add(archsimd.LoadUint8x16(p[1*step:]).ExtendToUint16().BitsToInt16().Mul(c1))
				acc = acc.Add(archsimd.LoadUint8x16(p[6*step:]).ExtendToUint16().BitsToInt16().Mul(c6))
				acc = acc.Add(archsimd.LoadUint8x16(p[7*step:]).ExtendToUint16().BitsToInt16().Mul(c7))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[2*step:]).ExtendToUint16().BitsToInt16().Mul(c2))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[3*step:]).ExtendToUint16().BitsToInt16().Mul(c3))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[4*step:]).ExtendToUint16().BitsToInt16().Mul(c4))
				acc = acc.AddSaturated(archsimd.LoadUint8x16(p[5*step:]).ExtendToUint16().BitsToInt16().Mul(c5))
			}
			acc = acc.ShiftAllRight(7)
			lo, hi := packBytes(acc.GetLo()).ReshapeToUint64s(), packBytes(acc.GetHi()).ReshapeToUint64s()
			lo.InterleaveLo(hi).ReshapeToUint8s().Store(dst[y*ds+x:])
		}
	}
	return true
}

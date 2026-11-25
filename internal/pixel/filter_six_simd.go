//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// Odd VP8 interpolation phases have zero outer taps. The six-tap caller
// proves the coefficient signs/bounds before omitting those unused inputs.
func filterFourNarrow8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) {
	if w != 4 && h > 1 && step == ss && filterVerticalFour8(src, ss, dst, ds, w, h, taps) {
		return
	}
	if step == 1 && w >= 16 && w%16 == 0 && filterHorizFour16(src, ss, dst, ds, w, h, taps) {
		return
	}
	c0, c1 := archsimd.BroadcastInt16x8(taps[0]), archsimd.BroadcastInt16x8(taps[1])
	c2, c3 := archsimd.BroadcastInt16x8(taps[2]), archsimd.BroadcastInt16x8(taps[3])
	bias := archsimd.BroadcastInt16x8(64)
	n := 8
	if w == 4 {
		n = 4
	}
	for y := range h {
		for x := 0; x < w; x += n {
			at := y*ss + x
			var p0, p1, p2, p3 archsimd.Int16x8
			if step == 1 && len(src)-at >= 16 {
				a := archsimd.LoadUint8x16(src[at:])
				z := archsimd.Uint8x16{}
				p0 = widen8(a)
				p1 = widen8(z.ConcatShiftBytesRight(a, 1))
				p2 = widen8(z.ConcatShiftBytesRight(a, 2))
				p3 = widen8(z.ConcatShiftBytesRight(a, 3))
			} else {
				p0, p1 = widen8(loadN(src[at:], n)), widen8(loadN(src[at+step:], n))
				p2, p3 = widen8(loadN(src[at+2*step:], n)), widen8(loadN(src[at+3*step:], n))
			}
			v := mulAdd16(p0.Mul(c0).Add(bias), p3, c3)
			v = v.AddSaturated(p1.Mul(c1)).AddSaturated(p2.Mul(c2))
			packed := packBytes(filterShift7(v))
			if n == 4 {
				binary.LittleEndian.PutUint32(dst[y*ds+x:], packed.ReshapeToUint32s().GetElem(0))
			} else {
				storeBytes8(dst[y*ds+x:], packed)
			}
		}
	}
}

// Nonnegative two-tap filters with total weight <=128 fit signed int16,
// including the rounding bias: 255*128+64 =32704. Eight output pixels can
// therefore share one multiply-add pipeline with no intermediate clipping.
func filterLinear8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) bool {
	if len(taps) != 2 || taps[0] < 0 || taps[1] < 0 || int(taps[0])+int(taps[1]) > 128 || w <= 0 || (w != 4 && w%8 != 0) || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	_ = src[(h-1)*ss+w-1+step]
	_ = dst[(h-1)*ds+w-1]
	if w >= 8 && (step == 1 || step == ss) && filterLinearFast(src, ss, dst, ds, w, h, step, uint8(taps[0]), uint8(taps[1])) {
		return true
	}
	c0, c1, bias := archsimd.BroadcastInt16x8(taps[0]), archsimd.BroadcastInt16x8(taps[1]), archsimd.BroadcastInt16x8(64)
	n := 8
	if w == 4 {
		n = 4
	}
	for y := range h {
		for x := 0; x < w; x += n {
			p := src[y*ss+x:]
			a, b := widen8(loadN(p, n)), widen8(loadN(p[step:], n))
			v := packBytes(filterShift7(mulAdd16(a.Mul(c0).Add(bias), b, c1)))
			if n == 4 {
				binary.LittleEndian.PutUint32(dst[y*ds+x:], v.ReshapeToUint32s().GetElem(0))
			} else {
				storeBytes8(dst[y*ds+x:], v)
			}
		}
	}
	return true
}

// Standard VP9 filters have at most 128 total negative weight, and every
// coefficient is in [-128,128]. Accumulating negative products first keeps
// the signed int16 sum exact. Subsequent positive products may saturate only
// above 32767; those results already clip to byte 255 after rounding by 7.
// This ordering therefore permits eight int16 lanes without premature
// saturation changing a result. Arbitrary filters retain the int32 kernel.
func filterNarrow8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) bool {
	if len(taps) != 8 || w <= 0 || (w != 4 && w%8 != 0) || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	mode := narrow8Mode(taps)
	if mode == 0 {
		return false
	}
	_ = src[(h-1)*ss+w-1+7*step]
	_ = dst[(h-1)*ds+w-1]
	if step == ss && filterVerticalNarrow8(src, ss, dst, ds, w, h, taps, mode) {
		return true
	}
	c0, c1, c2, c3 := archsimd.BroadcastInt16x8(taps[0]), archsimd.BroadcastInt16x8(taps[1]), archsimd.BroadcastInt16x8(taps[2]), archsimd.BroadcastInt16x8(taps[3])
	c4, c5, c6, c7 := archsimd.BroadcastInt16x8(taps[4]), archsimd.BroadcastInt16x8(taps[5]), archsimd.BroadcastInt16x8(taps[6]), archsimd.BroadcastInt16x8(taps[7])
	bias := archsimd.BroadcastInt16x8(64)
	n := 8
	if w == 4 {
		n = 4
	}
	for y := range h {
		for x := 0; x < w; x += n {
			p := src[y*ss+x:]
			var p0, p1, p2, p3, p4, p5, p6, p7 archsimd.Int16x8
			if step == 1 && len(p) >= 16 {
				a := archsimd.LoadUint8x16(p)
				z := archsimd.Uint8x16{}
				p0 = widen8(a)
				p1 = widen8(z.ConcatShiftBytesRight(a, 1))
				p2 = widen8(z.ConcatShiftBytesRight(a, 2))
				p3 = widen8(z.ConcatShiftBytesRight(a, 3))
				p4 = widen8(z.ConcatShiftBytesRight(a, 4))
				p5 = widen8(z.ConcatShiftBytesRight(a, 5))
				p6 = widen8(z.ConcatShiftBytesRight(a, 6))
				p7 = widen8(z.ConcatShiftBytesRight(a, 7))
			} else {
				p0, p1, p2, p3 = widen8(loadN(p, n)), widen8(loadN(p[step:], n)), widen8(loadN(p[2*step:], n)), widen8(loadN(p[3*step:], n))
				p4, p5, p6, p7 = widen8(loadN(p[4*step:], n)), widen8(loadN(p[5*step:], n)), widen8(loadN(p[6*step:], n)), widen8(loadN(p[7*step:], n))
			}

			acc := bias
			if mode == 1 {
				acc = mulAdd16(acc, p0, c0)
				acc = mulAdd16(acc, p2, c2)
				acc = mulAdd16(acc, p5, c5)
				acc = mulAdd16(acc, p7, c7)
				acc = acc.AddSaturated(p1.Mul(c1)).AddSaturated(p3.Mul(c3)).AddSaturated(p4.Mul(c4)).AddSaturated(p6.Mul(c6))
			} else {
				acc = mulAdd16(acc, p0, c0)
				acc = mulAdd16(acc, p1, c1)
				acc = mulAdd16(acc, p6, c6)
				acc = mulAdd16(acc, p7, c7)
				acc = acc.AddSaturated(p2.Mul(c2)).AddSaturated(p3.Mul(c3)).AddSaturated(p4.Mul(c4)).AddSaturated(p5.Mul(c5))
			}
			packed := packBytes(filterShift7(acc))
			if n == 4 {
				binary.LittleEndian.PutUint32(dst[y*ds+x:], packed.ReshapeToUint32s().GetElem(0))
			} else {
				storeBytes8(dst[y*ds+x:], packed)
			}
		}
	}
	return true
}

func narrow8Mode(taps []int16) int {
	t0, t1, t2, t3, t4, t5, t6, t7 := taps[0], taps[1], taps[2], taps[3], taps[4], taps[5], taps[6], taps[7]
	if min(t0, t1, t2, t3, t4, t5, t6, t7) < -128 || max(t0, t1, t2, t3, t4, t5, t6, t7) > 128 {
		return 0
	}
	if max(t0, t2, t5, t7) <= 0 && min(t1, t3, t4, t6) >= 0 {
		if -int(t0)-int(t2)-int(t5)-int(t7) <= 128 {
			return 1
		}
		return 0
	}
	if max(t0, t1, t6, t7) <= 0 && min(t2, t3, t4, t5) >= 0 && -int(t0)-int(t1)-int(t6)-int(t7) <= 128 {
		return 2
	}
	return 0
}

// VP8's two negative taps total at most 128. Accumulating those first keeps
// the signed int16 sum exact; subsequent positive saturation already clips
// to byte 255 after the final rounding shift.
func filterSixNarrow8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) bool {
	if len(taps) != 6 || w <= 0 || (w != 4 && w%8 != 0) || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	if min(taps[0], taps[2], taps[3], taps[5]) < 0 || max(taps[1], taps[4]) > 0 || min(taps[1], taps[4]) < -128 || max(taps[0], taps[2], taps[3], taps[5]) > 128 || -int(taps[1])-int(taps[4]) > 128 {
		return false
	}
	_ = src[(h-1)*ss+w-1+5*step]
	_ = dst[(h-1)*ds+w-1]
	if taps[0] == 0 && taps[5] == 0 {
		filterFourNarrow8(src[step:], ss, dst, ds, w, h, step, taps[1:5])
		return true
	}
	if w != 4 && h > 1 && step == ss && filterVerticalSix8(src, ss, dst, ds, w, h, taps) {
		return true
	}
	if step == 1 && w >= 16 && w%16 == 0 && filterHorizSix16(src, ss, dst, ds, w, h, taps) {
		return true
	}
	c0, c1, c2 := archsimd.BroadcastInt16x8(taps[0]), archsimd.BroadcastInt16x8(taps[1]), archsimd.BroadcastInt16x8(taps[2])
	c3, c4, c5 := archsimd.BroadcastInt16x8(taps[3]), archsimd.BroadcastInt16x8(taps[4]), archsimd.BroadcastInt16x8(taps[5])
	bias := archsimd.BroadcastInt16x8(64)
	n := 8
	if w == 4 {
		n = 4
	}
	for y := range h {
		for x := 0; x < w; x += n {
			at := y*ss + x
			var p0, p1, p2, p3, p4, p5 archsimd.Int16x8
			if step == 1 && len(src)-at >= 16 {
				a := archsimd.LoadUint8x16(src[at:])
				z := archsimd.Uint8x16{}
				p0 = widen8(a)
				p1 = widen8(z.ConcatShiftBytesRight(a, 1))
				p2 = widen8(z.ConcatShiftBytesRight(a, 2))
				p3 = widen8(z.ConcatShiftBytesRight(a, 3))
				p4 = widen8(z.ConcatShiftBytesRight(a, 4))
				p5 = widen8(z.ConcatShiftBytesRight(a, 5))
			} else {
				p0, p1, p2 = widen8(loadN(src[at:], n)), widen8(loadN(src[at+step:], n)), widen8(loadN(src[at+2*step:], n))
				p3, p4, p5 = widen8(loadN(src[at+3*step:], n)), widen8(loadN(src[at+4*step:], n)), widen8(loadN(src[at+5*step:], n))
			}
			v := mulAdd16(p1.Mul(c1).Add(bias), p4, c4)
			v = v.AddSaturated(p0.Mul(c0)).AddSaturated(p2.Mul(c2)).AddSaturated(p3.Mul(c3)).AddSaturated(p5.Mul(c5))
			b := packBytes(filterShift7(v))
			if n == 4 {
				binary.LittleEndian.PutUint32(dst[y*ds+x:], b.ReshapeToUint32s().GetElem(0))
			} else {
				storeBytes8(dst[y*ds+x:], b)
			}
		}
	}
	return true
}

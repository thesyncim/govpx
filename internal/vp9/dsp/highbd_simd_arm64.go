//go:build goexperiment.simd && arm64 && !purego

package dsp

import "simd/archsimd"

// High-bit-depth (10/12-bit) ARM64 NEON SIMD kernels using Go's simd/archsimd.
// Every kernel is bit-exact with the scalar libvpx C ports in highbd_convolve.go
// and accelerates both VP9 Profile 2 (4:2:0) and Profile 3 (4:2:2/4:4:4).

// highbdConvolveAvgSIMD averages w x h uint16 blocks using NEON URHADD
// ((a + b + 1) >> 1) 8 pixels at a time.
func highbdConvolveAvgSIMD(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int, w, h int,
) bool {
	if w < 8 || w&7 != 0 {
		return false
	}
	for range h {
		for x := 0; x < w; x += 8 {
			sv := archsimd.LoadUint16x8(src[srcOff+x:])
			dv := archsimd.LoadUint16x8(dst[dstOff+x:])
			dv.Average(sv).Store(dst[dstOff+x:])
		}
		srcOff += srcStride
		dstOff += dstStride
	}
	return true
}

// highbdConvolve8HorizSIMD runs an 8-wide unscaled (xStepQ4 == 16) 8-tap
// horizontal filter on uint16 pixels with int32 accumulation, rounding >> 7,
// and [0, (1<<bd)-1] saturation.
func highbdConvolve8HorizSIMD(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	xFilters *[16][8]int16, x0Q4, xStepQ4, w, h, bd int, avg bool,
) bool {
	if xStepQ4 != 16 || w < 8 || w&7 != 0 {
		return false
	}
	f := &xFilters[x0Q4&highbdSubpelMask]
	t0 := archsimd.BroadcastInt16x8(f[0])
	t1 := archsimd.BroadcastInt16x8(f[1])
	t2 := archsimd.BroadcastInt16x8(f[2])
	t3 := archsimd.BroadcastInt16x8(f[3])
	t4 := archsimd.BroadcastInt16x8(f[4])
	t5 := archsimd.BroadcastInt16x8(f[5])
	t6 := archsimd.BroadcastInt16x8(f[6])
	t7 := archsimd.BroadcastInt16x8(f[7])
	round := archsimd.BroadcastInt32x4(1 << (highbdFilterBits - 1))
	maxVal := archsimd.BroadcastInt32x4(int32((1 << uint(bd)) - 1))

	srcOff = srcOff - (highbdSubpelTaps/2 - 1) + (x0Q4 >> highbdSubpelBits)
	for range h {
		for x := 0; x < w; x += 8 {
			base := srcOff + x
			s0 := archsimd.LoadUint16x8(src[base+0:]).BitsToInt16()
			s1 := archsimd.LoadUint16x8(src[base+1:]).BitsToInt16()
			s2 := archsimd.LoadUint16x8(src[base+2:]).BitsToInt16()
			s3 := archsimd.LoadUint16x8(src[base+3:]).BitsToInt16()
			s4 := archsimd.LoadUint16x8(src[base+4:]).BitsToInt16()
			s5 := archsimd.LoadUint16x8(src[base+5:]).BitsToInt16()
			s6 := archsimd.LoadUint16x8(src[base+6:]).BitsToInt16()
			s7 := archsimd.LoadUint16x8(src[base+7:]).BitsToInt16()

			lo := s0.MulWidenLo(t0).
				Add(s1.MulWidenLo(t1)).
				Add(s2.MulWidenLo(t2)).
				Add(s3.MulWidenLo(t3)).
				Add(s4.MulWidenLo(t4)).
				Add(s5.MulWidenLo(t5)).
				Add(s6.MulWidenLo(t6)).
				Add(s7.MulWidenLo(t7)).
				Add(round).ShiftAllRight(highbdFilterBits).
				Min(maxVal).SaturateToUint16()

			hi := s0.HiToLo().MulWidenLo(t0).
				Add(s1.HiToLo().MulWidenLo(t1)).
				Add(s2.HiToLo().MulWidenLo(t2)).
				Add(s3.HiToLo().MulWidenLo(t3)).
				Add(s4.HiToLo().MulWidenLo(t4)).
				Add(s5.HiToLo().MulWidenLo(t5)).
				Add(s6.HiToLo().MulWidenLo(t6)).
				Add(s7.HiToLo().MulWidenLo(t7)).
				Add(round).ShiftAllRight(highbdFilterBits).
				Min(maxVal).SaturateToUint16()

			res := lo.ReshapeToUint64s().InterleaveLo(hi.ReshapeToUint64s()).ReshapeToUint16s()
			if avg {
				dv := archsimd.LoadUint16x8(dst[dstOff+x:])
				res = dv.Average(res)
			}
			res.Store(dst[dstOff+x:])
		}
		srcOff += srcStride
		dstOff += dstStride
	}
	return true
}

// highbdConvolve8VertSIMD runs an 8-wide unscaled (yStepQ4 == 16) 8-tap
// vertical filter on uint16 pixels with sliding row registers.
func highbdConvolve8VertSIMD(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	yFilters *[16][8]int16, y0Q4, yStepQ4, w, h, bd int, avg bool,
) bool {
	if yStepQ4 != 16 || w < 8 || w&7 != 0 {
		return false
	}
	f := &yFilters[y0Q4&highbdSubpelMask]
	t0 := archsimd.BroadcastInt16x8(f[0])
	t1 := archsimd.BroadcastInt16x8(f[1])
	t2 := archsimd.BroadcastInt16x8(f[2])
	t3 := archsimd.BroadcastInt16x8(f[3])
	t4 := archsimd.BroadcastInt16x8(f[4])
	t5 := archsimd.BroadcastInt16x8(f[5])
	t6 := archsimd.BroadcastInt16x8(f[6])
	t7 := archsimd.BroadcastInt16x8(f[7])
	round := archsimd.BroadcastInt32x4(1 << (highbdFilterBits - 1))
	maxVal := archsimd.BroadcastInt32x4(int32((1 << uint(bd)) - 1))

	srcBase := srcOff - srcStride*(highbdSubpelTaps/2-1) + (y0Q4>>highbdSubpelBits)*srcStride
	for x := 0; x < w; x += 8 {
		sp := srcBase + x
		s0 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
		sp += srcStride
		s1 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
		sp += srcStride
		s2 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
		sp += srcStride
		s3 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
		sp += srcStride
		s4 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
		sp += srcStride
		s5 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
		sp += srcStride
		s6 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
		sp += srcStride

		dp := dstOff + x
		for range h {
			s7 := archsimd.LoadUint16x8(src[sp:]).BitsToInt16()
			sp += srcStride

			lo := s0.MulWidenLo(t0).
				Add(s1.MulWidenLo(t1)).
				Add(s2.MulWidenLo(t2)).
				Add(s3.MulWidenLo(t3)).
				Add(s4.MulWidenLo(t4)).
				Add(s5.MulWidenLo(t5)).
				Add(s6.MulWidenLo(t6)).
				Add(s7.MulWidenLo(t7)).
				Add(round).ShiftAllRight(highbdFilterBits).
				Min(maxVal).SaturateToUint16()

			hi := s0.HiToLo().MulWidenLo(t0).
				Add(s1.HiToLo().MulWidenLo(t1)).
				Add(s2.HiToLo().MulWidenLo(t2)).
				Add(s3.HiToLo().MulWidenLo(t3)).
				Add(s4.HiToLo().MulWidenLo(t4)).
				Add(s5.HiToLo().MulWidenLo(t5)).
				Add(s6.HiToLo().MulWidenLo(t6)).
				Add(s7.HiToLo().MulWidenLo(t7)).
				Add(round).ShiftAllRight(highbdFilterBits).
				Min(maxVal).SaturateToUint16()

			res := lo.ReshapeToUint64s().InterleaveLo(hi.ReshapeToUint64s()).ReshapeToUint16s()
			if avg {
				dv := archsimd.LoadUint16x8(dst[dp:])
				res = dv.Average(res)
			}
			res.Store(dst[dp:])
			dp += dstStride

			s0, s1, s2, s3, s4, s5, s6 = s1, s2, s3, s4, s5, s6, s7
		}
	}
	return true
}

// HighbdSAD computes the sum of absolute differences over n uint16 samples
// (values in 0..4095) using 8-wide NEON SIMD.
func HighbdSAD(a, b []uint16, n int) int {
	var acc archsimd.Int32x4
	i := 0
	for ; i+8 <= n; i += 8 {
		va := archsimd.LoadUint16x8(a[i:]).BitsToInt16()
		vb := archsimd.LoadUint16x8(b[i:]).BitsToInt16()
		diff := va.Sub(vb).Abs()
		acc = acc.Add(diff.ExtendLo4ToInt32()).Add(diff.HiToLo().ExtendLo4ToInt32())
	}
	sum := int(acc.ReduceSum())
	for ; i < n; i++ {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		sum += d
	}
	return sum
}

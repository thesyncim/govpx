package dsp

import "github.com/thesyncim/govpx/internal/vp9/tables"

// VP9 8-tap subpel convolve kernels. Ported from libvpx v1.16.0
// vpx_dsp/vpx_convolve.c (the "_c" reference implementations only —
// the SIMD path lives elsewhere). The fractional MV is split into
// (x0_q4, x_step_q4) / (y0_q4, y_step_q4) at 1/16-pel precision; the
// integer part indexes into the source plane and the fractional part
// selects one of 16 InterpKernel rows.

// convolveHoriz applies a single horizontal pass with the supplied
// subpel filter table, stepping the fractional x by xStepQ4 per output
// column. Matches libvpx's convolve_horiz line-for-line; src is biased
// back by SUBPEL_TAPS/2 - 1 so the kernel center aligns with x_q4 >> 4.
func convolveHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	xFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - (tables.SubpelTaps/2 - 1)
	for y := 0; y < h; y++ {
		xQ4 := x0Q4
		rowSrc := srcStart + y*srcStride
		rowDst := y * dstStride
		for x := 0; x < w; x++ {
			base := rowSrc + (xQ4 >> tables.SubpelBits)
			filter := &xFilters[xQ4&tables.SubpelMask]
			sum := 0
			for k := 0; k < tables.SubpelTaps; k++ {
				sum += int(src[base+k]) * int(filter[k])
			}
			dst[rowDst+x] = clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits))
			xQ4 += xStepQ4
		}
	}
}

// convolveVert applies a single vertical pass. Matches convolve_vert in
// libvpx.
func convolveVert(src []byte, srcStride int, dst []byte, dstStride int,
	yFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	y0Q4, yStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - srcStride*(tables.SubpelTaps/2-1)
	for x := 0; x < w; x++ {
		yQ4 := y0Q4
		for y := 0; y < h; y++ {
			base := srcStart + (yQ4>>tables.SubpelBits)*srcStride
			filter := &yFilters[yQ4&tables.SubpelMask]
			sum := 0
			for k := 0; k < tables.SubpelTaps; k++ {
				sum += int(src[base+k*srcStride]) * int(filter[k])
			}
			dst[y*dstStride+x] = clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits))
			yQ4 += yStepQ4
		}
		srcStart++
	}
}

// convolveAvgHoriz / convolveAvgVert blend the filtered result with the
// pre-existing dst pixel (used when libvpx caller wants 2-reference
// averaging in inter prediction). Matches convolve_avg_horiz/vert.
func convolveAvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	xFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - (tables.SubpelTaps/2 - 1)
	for y := 0; y < h; y++ {
		xQ4 := x0Q4
		rowSrc := srcStart + y*srcStride
		rowDst := y * dstStride
		for x := 0; x < w; x++ {
			base := rowSrc + (xQ4 >> tables.SubpelBits)
			filter := &xFilters[xQ4&tables.SubpelMask]
			sum := 0
			for k := 0; k < tables.SubpelTaps; k++ {
				sum += int(src[base+k]) * int(filter[k])
			}
			c := int(dst[rowDst+x]) + int(clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits)))
			dst[rowDst+x] = uint8((c + 1) >> 1)
			xQ4 += xStepQ4
		}
	}
}

func convolveAvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	yFilters *[tables.SubpelShifts][tables.SubpelTaps]int16,
	y0Q4, yStepQ4, w, h, srcOffset int,
) {
	srcStart := srcOffset - srcStride*(tables.SubpelTaps/2-1)
	for x := 0; x < w; x++ {
		yQ4 := y0Q4
		for y := 0; y < h; y++ {
			base := srcStart + (yQ4>>tables.SubpelBits)*srcStride
			filter := &yFilters[yQ4&tables.SubpelMask]
			sum := 0
			for k := 0; k < tables.SubpelTaps; k++ {
				sum += int(src[base+k*srcStride]) * int(filter[k])
			}
			c := int(dst[y*dstStride+x]) + int(clipPixel(roundPowerOfTwo(int32(sum), tables.FilterBits)))
			dst[y*dstStride+x] = uint8((c + 1) >> 1)
			yQ4 += yStepQ4
		}
		srcStart++
	}
}

func clipPixel(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// VpxConvolve8Horiz applies the horizontal 8-tap subpel filter. Mirrors
// vpx_convolve8_horiz_c. src is the source buffer with `srcOffset`
// chosen so src[srcOffset] is the top-left subpel-anchor pixel (the
// caller-side equivalent of the `src` pointer libvpx receives).
func VpxConvolve8Horiz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	convolveHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
}

// VpxConvolve8AvgHoriz mirrors vpx_convolve8_avg_horiz_c.
func VpxConvolve8AvgHoriz(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = y0Q4
	_ = yStepQ4
	convolveAvgHoriz(src, srcStride, dst, dstStride, filter, x0Q4, xStepQ4, w, h, srcOffset)
}

// VpxConvolve8Vert mirrors vpx_convolve8_vert_c.
func VpxConvolve8Vert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	convolveVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
}

// VpxConvolve8AvgVert mirrors vpx_convolve8_avg_vert_c.
func VpxConvolve8AvgVert(src []byte, srcStride int, dst []byte, dstStride int,
	filter *[tables.SubpelShifts][tables.SubpelTaps]int16,
	x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, srcOffset int,
) {
	_ = x0Q4
	_ = xStepQ4
	convolveAvgVert(src, srcStride, dst, dstStride, filter, y0Q4, yStepQ4, w, h, srcOffset)
}

// VpxConvolveCopy mirrors vpx_convolve_copy_c — a straight memcpy of
// w x h pixels from src to dst at the given strides.
func VpxConvolveCopy(src []byte, srcStride int, dst []byte, dstStride, w, h, srcOffset int) {
	for y := 0; y < h; y++ {
		copy(dst[y*dstStride:y*dstStride+w], src[srcOffset+y*srcStride:srcOffset+y*srcStride+w])
	}
}

// VpxConvolveAvg mirrors vpx_convolve_avg_c — blend src and dst by
// rounded mean.
func VpxConvolveAvg(src []byte, srcStride int, dst []byte, dstStride, w, h, srcOffset int) {
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := int(src[srcOffset+y*srcStride+x]) + int(dst[y*dstStride+x])
			dst[y*dstStride+x] = uint8((c + 1) >> 1)
		}
	}
}

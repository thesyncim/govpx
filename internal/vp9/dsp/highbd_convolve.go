package dsp

// High-bit-depth (10-bit / 12-bit) 8-tap subpel convolution kernels.
// Ported verbatim from libvpx v1.16.0 vpx_dsp/vpx_convolve.c.

const (
	highbdSubpelBits = 4
	highbdSubpelMask = (1 << highbdSubpelBits) - 1
	highbdSubpelTaps = 8
	highbdFilterBits = 7
)

func clipPixelHighbd(val int, bd int) uint16 {
	if val < 0 {
		return 0
	}
	maxVal := (1 << uint(bd)) - 1
	if val > maxVal {
		return uint16(maxVal)
	}
	return uint16(val)
}

func highbdRoundPowerOfTwo(val int, bits uint) int {
	return (val + (1 << (bits - 1))) >> bits
}

func highbdConvolveHoriz(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	xFilters *[16][8]int16, x0Q4, xStepQ4, w, h, bd int,
) {
	srcOff -= highbdSubpelTaps/2 - 1
	for range h {
		xQ4 := x0Q4
		for x := range w {
			s := srcOff + (xQ4 >> highbdSubpelBits)
			f := &xFilters[xQ4&highbdSubpelMask]
			sum := int(src[s+0])*int(f[0]) +
				int(src[s+1])*int(f[1]) +
				int(src[s+2])*int(f[2]) +
				int(src[s+3])*int(f[3]) +
				int(src[s+4])*int(f[4]) +
				int(src[s+5])*int(f[5]) +
				int(src[s+6])*int(f[6]) +
				int(src[s+7])*int(f[7])
			dst[dstOff+x] = clipPixelHighbd(highbdRoundPowerOfTwo(sum, highbdFilterBits), bd)
			xQ4 += xStepQ4
		}
		srcOff += srcStride
		dstOff += dstStride
	}
}

func highbdConvolveAvgHoriz(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	xFilters *[16][8]int16, x0Q4, xStepQ4, w, h, bd int,
) {
	srcOff -= highbdSubpelTaps/2 - 1
	for range h {
		xQ4 := x0Q4
		for x := range w {
			s := srcOff + (xQ4 >> highbdSubpelBits)
			f := &xFilters[xQ4&highbdSubpelMask]
			sum := int(src[s+0])*int(f[0]) +
				int(src[s+1])*int(f[1]) +
				int(src[s+2])*int(f[2]) +
				int(src[s+3])*int(f[3]) +
				int(src[s+4])*int(f[4]) +
				int(src[s+5])*int(f[5]) +
				int(src[s+6])*int(f[6]) +
				int(src[s+7])*int(f[7])
			p := int(clipPixelHighbd(highbdRoundPowerOfTwo(sum, highbdFilterBits), bd))
			dst[dstOff+x] = uint16(highbdRoundPowerOfTwo(int(dst[dstOff+x])+p, 1))
			xQ4 += xStepQ4
		}
		srcOff += srcStride
		dstOff += dstStride
	}
}

func highbdConvolveVert(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	yFilters *[16][8]int16, y0Q4, yStepQ4, w, h, bd int,
) {
	srcOff -= srcStride * (highbdSubpelTaps/2 - 1)
	for x := range w {
		yQ4 := y0Q4
		for y := range h {
			s := srcOff + x + (yQ4>>highbdSubpelBits)*srcStride
			f := &yFilters[yQ4&highbdSubpelMask]
			sum := int(src[s+0*srcStride])*int(f[0]) +
				int(src[s+1*srcStride])*int(f[1]) +
				int(src[s+2*srcStride])*int(f[2]) +
				int(src[s+3*srcStride])*int(f[3]) +
				int(src[s+4*srcStride])*int(f[4]) +
				int(src[s+5*srcStride])*int(f[5]) +
				int(src[s+6*srcStride])*int(f[6]) +
				int(src[s+7*srcStride])*int(f[7])
			dst[dstOff+y*dstStride+x] = clipPixelHighbd(highbdRoundPowerOfTwo(sum, highbdFilterBits), bd)
			yQ4 += yStepQ4
		}
	}
}

func highbdConvolveAvgVert(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	yFilters *[16][8]int16, y0Q4, yStepQ4, w, h, bd int,
) {
	srcOff -= srcStride * (highbdSubpelTaps/2 - 1)
	for x := range w {
		yQ4 := y0Q4
		for y := range h {
			s := srcOff + x + (yQ4>>highbdSubpelBits)*srcStride
			f := &yFilters[yQ4&highbdSubpelMask]
			sum := int(src[s+0*srcStride])*int(f[0]) +
				int(src[s+1*srcStride])*int(f[1]) +
				int(src[s+2*srcStride])*int(f[2]) +
				int(src[s+3*srcStride])*int(f[3]) +
				int(src[s+4*srcStride])*int(f[4]) +
				int(src[s+5*srcStride])*int(f[5]) +
				int(src[s+6*srcStride])*int(f[6]) +
				int(src[s+7*srcStride])*int(f[7])
			p := int(clipPixelHighbd(highbdRoundPowerOfTwo(sum, highbdFilterBits), bd))
			di := dstOff + y*dstStride + x
			dst[di] = uint16(highbdRoundPowerOfTwo(int(dst[di])+p, 1))
			yQ4 += yStepQ4
		}
	}
}

// HighbdConvolveCopy copies a w x h uint16 block.
func HighbdConvolveCopy(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int, w, h int,
) {
	for range h {
		copy(dst[dstOff:dstOff+w], src[srcOff:srcOff+w])
		srcOff += srcStride
		dstOff += dstStride
	}
}

// HighbdConvolveAvg averages a w x h uint16 block into dst.
func HighbdConvolveAvg(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int, w, h int,
) {
	if highbdConvolveAvgSIMD(src, srcOff, srcStride, dst, dstOff, dstStride, w, h) {
		return
	}
	for range h {
		for x := range w {
			dst[dstOff+x] = uint16(highbdRoundPowerOfTwo(int(dst[dstOff+x])+int(src[srcOff+x]), 1))
		}
		srcOff += srcStride
		dstOff += dstStride
	}
}

// HighbdConvolve8Horiz applies horizontal 8-tap subpel filtering.
func HighbdConvolve8Horiz(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	filter *[16][8]int16, x0Q4, xStepQ4, w, h, bd int,
) {
	if highbdConvolve8HorizSIMD(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, x0Q4, xStepQ4, w, h, bd, false) {
		return
	}
	highbdConvolveHoriz(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, x0Q4, xStepQ4, w, h, bd)
}

// HighbdConvolve8AvgHoriz applies horizontal 8-tap subpel filtering and averages into dst.
func HighbdConvolve8AvgHoriz(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	filter *[16][8]int16, x0Q4, xStepQ4, w, h, bd int,
) {
	if highbdConvolve8HorizSIMD(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, x0Q4, xStepQ4, w, h, bd, true) {
		return
	}
	highbdConvolveAvgHoriz(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, x0Q4, xStepQ4, w, h, bd)
}

// HighbdConvolve8Vert applies vertical 8-tap subpel filtering.
func HighbdConvolve8Vert(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	filter *[16][8]int16, y0Q4, yStepQ4, w, h, bd int,
) {
	if highbdConvolve8VertSIMD(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, y0Q4, yStepQ4, w, h, bd, false) {
		return
	}
	highbdConvolveVert(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, y0Q4, yStepQ4, w, h, bd)
}

// HighbdConvolve8AvgVert applies vertical 8-tap subpel filtering and averages into dst.
func HighbdConvolve8AvgVert(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	filter *[16][8]int16, y0Q4, yStepQ4, w, h, bd int,
) {
	if highbdConvolve8VertSIMD(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, y0Q4, yStepQ4, w, h, bd, true) {
		return
	}
	highbdConvolveAvgVert(src, srcOff, srcStride, dst, dstOff, dstStride,
		filter, y0Q4, yStepQ4, w, h, bd)
}

// HighbdConvolve8 applies separable 2D 8-tap subpel filtering.
func HighbdConvolve8(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	filter *[16][8]int16, x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, bd int,
) {
	var temp [64 * 135]uint16
	intermediateHeight := (((h-1)*yStepQ4 + y0Q4) >> highbdSubpelBits) + highbdSubpelTaps
	HighbdConvolve8Horiz(src, srcOff-srcStride*(highbdSubpelTaps/2-1), srcStride,
		temp[:], 0, 64, filter, x0Q4, xStepQ4, w, intermediateHeight, bd)
	HighbdConvolve8Vert(temp[:], 64*(highbdSubpelTaps/2-1), 64,
		dst, dstOff, dstStride, filter, y0Q4, yStepQ4, w, h, bd)
}

// HighbdConvolve8Avg applies separable 2D 8-tap subpel filtering and averages into dst.
func HighbdConvolve8Avg(src []uint16, srcOff, srcStride int,
	dst []uint16, dstOff, dstStride int,
	filter *[16][8]int16, x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, bd int,
) {
	var temp [64 * 64]uint16
	HighbdConvolve8(src, srcOff, srcStride, temp[:], 0, 64,
		filter, x0Q4, xStepQ4, y0Q4, yStepQ4, w, h, bd)
	HighbdConvolveAvg(temp[:], 0, 64, dst, dstOff, dstStride, w, h)
}

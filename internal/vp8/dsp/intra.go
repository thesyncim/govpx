package dsp

// Ported from libvpx v1.16.0 vpx_dsp/intrapred.c and
// vp8/common/reconintra.c.

func IntraDCPredict16x16(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	intraDCPredict16x16(dst, dstStride, above, left, upAvailable, leftAvailable)
}

func IntraDCPredict8x8(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	intraDCPredict8x8(dst, dstStride, above, left, upAvailable, leftAvailable)
}

func IntraVerticalPredict16x16(dst []byte, dstStride int, above []byte) {
	intraVerticalPredict16x16(dst, dstStride, above)
}

func IntraVerticalPredict8x8(dst []byte, dstStride int, above []byte) {
	intraVerticalPredict8x8(dst, dstStride, above)
}

func IntraHorizontalPredict16x16(dst []byte, dstStride int, left []byte) {
	intraHorizontalPredict16x16(dst, dstStride, left)
}

func IntraHorizontalPredict8x8(dst []byte, dstStride int, left []byte) {
	intraHorizontalPredict8x8(dst, dstStride, left)
}

func IntraTMPredict16x16(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intraTMPredict16x16(dst, dstStride, above, left, topLeft)
}

func IntraTMPredict8x8(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intraTMPredict8x8(dst, dstStride, above, left, topLeft)
}

func intraDCPredictScalar(dst []byte, dstStride int, above []byte, left []byte, size int, upAvailable bool, leftAvailable bool) {
	dc := 128

	if upAvailable && leftAvailable {
		ab := above[:size]
		le := left[:size]
		sum := 0
		for i := range size {
			sum += int(ab[i]) + int(le[i])
		}
		dc = (sum + size) / (2 * size)
	} else if upAvailable {
		ab := above[:size]
		sum := 0
		for i := range size {
			sum += int(ab[i])
		}
		dc = (sum + (size >> 1)) / size
	} else if leftAvailable {
		le := left[:size]
		sum := 0
		for i := range size {
			sum += int(le[i])
		}
		dc = (sum + (size >> 1)) / size
	}

	fillBlock(dst, dstStride, size, byte(dc))
}

func intraDCPredictWindowOK(dst []byte, dstStride int, above []byte, left []byte, size int, upAvailable bool, leftAvailable bool) bool {
	if !dspWindowOK(dst, dstStride, size, size) {
		return false
	}
	if upAvailable && len(above) < size {
		return false
	}
	if leftAvailable && len(left) < size {
		return false
	}
	return true
}

func intraPredictWindowOK(dst []byte, dstStride int, edge []byte, size int) bool {
	return dspWindowOK(dst, dstStride, size, size) && len(edge) >= size
}

func intraTMPredictWindowOK(dst []byte, dstStride int, above []byte, left []byte, size int) bool {
	return dspWindowOK(dst, dstStride, size, size) && len(above) >= size && len(left) >= size
}

func intraVerticalPredictScalar(dst []byte, dstStride int, above []byte, size int) {
	_ = above[size-1]
	_ = dst[(size-1)*dstStride+size-1]

	for y := range size {
		copy(dst[y*dstStride:y*dstStride+size], above[:size])
	}
}

func intraHorizontalPredictScalar(dst []byte, dstStride int, left []byte, size int) {
	_ = left[size-1]
	_ = dst[(size-1)*dstStride+size-1]

	for y := range size {
		row := y * dstStride
		v := left[y]
		for x := range size {
			dst[row+x] = v
		}
	}
}

func intraTMPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte, size int) {
	_ = above[size-1]
	_ = left[size-1]
	_ = dst[(size-1)*dstStride+size-1]

	base := int(topLeft)
	for y := range size {
		row := y * dstStride
		leftDelta := int(left[y]) - base
		for x := range size {
			dst[row+x] = ClipPixel(leftDelta + int(above[x]))
		}
	}
}

func fillBlock(dst []byte, dstStride int, size int, v byte) {
	_ = dst[(size-1)*dstStride+size-1]

	for y := range size {
		row := y * dstStride
		for x := range size {
			dst[row+x] = v
		}
	}
}

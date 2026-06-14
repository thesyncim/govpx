package dsp

// Scalar bounds guard for libvpx v1.16.0 VP8 DSP kernels before dispatching to
// source-shaped C/ASM-equivalent predictors and variance/SAD helpers.
func dspWindowOK(buf []byte, stride, w, h int) bool {
	if stride < 0 || w <= 0 || h <= 0 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	rows := h - 1
	if rows != 0 && stride > maxInt/rows {
		return false
	}
	rowSpan := rows * stride
	if w > maxInt-rowSpan {
		return false
	}
	limit := rowSpan + w
	return limit <= len(buf)
}

func dspSIMDPredictWindowOK(src []byte, srcStride, srcLoadWidth, srcRows int, dst []byte, dstStride, dstWidth, dstRows int) bool {
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	return dspWindowOK(src, srcStride, srcLoadWidth, srcRows) &&
		dspWindowOK(dst, dstStride, dstWidth, dstRows)
}

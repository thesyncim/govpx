package dsp

// SubtractBlockNonZero writes src - pred into out and reports whether any
// residual sample is non-zero. It mirrors libvpx vpx_subtract_block for VP9
// profile-0 byte planes, with the non-zero test fused for encoder callers.
func SubtractBlockNonZero(src []uint8, srcOff, srcStride int,
	pred []uint8, predOff, predStride int,
	out []int16, outOff, outStride int,
	w, h int,
) (nonZero bool, ok bool) {
	if w <= 0 || h <= 0 ||
		!subtractByteWindowOK(src, srcOff, srcStride, w, h) ||
		!subtractByteWindowOK(pred, predOff, predStride, w, h) ||
		!subtractInt16WindowOK(out, outOff, outStride, w, h) {
		return false, false
	}
	if nonZero, ok := subtractBlockNonZeroFast(src, srcOff, srcStride,
		pred, predOff, predStride, out, outOff, outStride, w, h); ok {
		return nonZero, true
	}
	return subtractBlockNonZeroScalar(src, srcOff, srcStride,
		pred, predOff, predStride, out, outOff, outStride, w, h), true
}

func subtractByteWindowOK(buf []uint8, off, stride, w, h int) bool {
	if off < 0 || stride < 0 {
		return false
	}
	limit := off + (h-1)*stride + w
	return limit >= off && limit <= len(buf)
}

func subtractInt16WindowOK(buf []int16, off, stride, w, h int) bool {
	if off < 0 || stride < 0 {
		return false
	}
	limit := off + (h-1)*stride + w
	return limit >= off && limit <= len(buf)
}

func subtractBlockNonZeroScalar(src []uint8, srcOff, srcStride int,
	pred []uint8, predOff, predStride int,
	out []int16, outOff, outStride int,
	w, h int,
) bool {
	diffMask := 0
	for y := range h {
		srcRow := srcOff + y*srcStride
		predRow := predOff + y*predStride
		outRow := outOff + y*outStride
		for x := range w {
			diff := int(src[srcRow+x]) - int(pred[predRow+x])
			out[outRow+x] = int16(diff)
			diffMask |= diff
		}
	}
	return diffMask != 0
}

package dsp

func dspReadWindowOK(buf []uint8, off, stride, w, h int) bool {
	if off < 0 || stride < 0 || w <= 0 || h <= 0 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	rows := h - 1
	if rows != 0 && stride > maxInt/rows {
		return false
	}
	rowSpan := rows * stride
	if off > maxInt-rowSpan {
		return false
	}
	startLast := off + rowSpan
	if w > maxInt-startLast {
		return false
	}
	limit := startLast + w
	return limit <= len(buf)
}

func dspSubpelReadWindowOK(buf []uint8, off, stride, w, h int) bool {
	maxInt := int(^uint(0) >> 1)
	if w == maxInt || h == maxInt {
		return false
	}
	return dspReadWindowOK(buf, off, stride, w+1, h+1)
}

package dsp

// High-bit-depth (10-bit / 12-bit) VP9 in-loop deblocking filters.
// Ported verbatim from libvpx v1.16.0 vpx_dsp/loopfilter.c.

func signedCharClampHigh(t, bd int) int16 {
	var lo, hi int
	switch bd {
	case 10:
		lo, hi = -128*4, 128*4-1
	case 12:
		lo, hi = -128*16, 128*16-1
	default:
		lo, hi = -128, 128-1
	}
	if t < lo {
		return int16(lo)
	}
	if t > hi {
		return int16(hi)
	}
	return int16(t)
}

func highbdAbsDiff(a, b uint16) int {
	d := int(a) - int(b)
	if d < 0 {
		return -d
	}
	return d
}

func highbdFilterMask(limit, blimit uint8,
	p3, p2, p1, p0, q0, q1, q2, q3 uint16, bd int,
) int8 {
	shift := uint(bd - 8)
	limit16 := int(limit) << shift
	blimit16 := int(blimit) << shift
	if highbdAbsDiff(p3, p2) > limit16 ||
		highbdAbsDiff(p2, p1) > limit16 ||
		highbdAbsDiff(p1, p0) > limit16 ||
		highbdAbsDiff(q1, q0) > limit16 ||
		highbdAbsDiff(q2, q1) > limit16 ||
		highbdAbsDiff(q3, q2) > limit16 ||
		highbdAbsDiff(p0, q0)*2+highbdAbsDiff(p1, q1)/2 > blimit16 {
		return 0
	}
	return -1
}

func highbdFlatMask4(thresh uint8,
	p3, p2, p1, p0, q0, q1, q2, q3 uint16, bd int,
) int8 {
	thresh16 := int(thresh) << uint(bd-8)
	if highbdAbsDiff(p1, p0) > thresh16 ||
		highbdAbsDiff(q1, q0) > thresh16 ||
		highbdAbsDiff(p2, p0) > thresh16 ||
		highbdAbsDiff(q2, q0) > thresh16 ||
		highbdAbsDiff(p3, p0) > thresh16 ||
		highbdAbsDiff(q3, q0) > thresh16 {
		return 0
	}
	return -1
}

func highbdFlatMask5(thresh uint8,
	p4, p3, p2, p1, p0, q0, q1, q2, q3, q4 uint16, bd int,
) int8 {
	if highbdFlatMask4(thresh, p3, p2, p1, p0, q0, q1, q2, q3, bd) == 0 {
		return 0
	}
	thresh16 := int(thresh) << uint(bd-8)
	if highbdAbsDiff(p4, p0) > thresh16 || highbdAbsDiff(q4, q0) > thresh16 {
		return 0
	}
	return -1
}

func highbdHevMask(thresh uint8, p1, p0, q0, q1 uint16, bd int) int16 {
	thresh16 := int(thresh) << uint(bd-8)
	if highbdAbsDiff(p1, p0) > thresh16 || highbdAbsDiff(q1, q0) > thresh16 {
		return -1
	}
	return 0
}

func highbdFilter4(mask int8, thresh uint8,
	op1, op0, oq0, oq1 *uint16, bd int,
) {
	shift := uint(bd - 8)
	bias := int16(0x80 << shift)
	ps1 := int16(*op1) - bias
	ps0 := int16(*op0) - bias
	qs0 := int16(*oq0) - bias
	qs1 := int16(*oq1) - bias
	hev := highbdHevMask(thresh, *op1, *op0, *oq0, *oq1, bd)

	filter := signedCharClampHigh(int(ps1)-int(qs1), bd) & hev
	filter = signedCharClampHigh(int(filter)+3*(int(qs0)-int(ps0)), bd) & int16(mask)

	filter1 := signedCharClampHigh(int(filter)+4, bd) >> 3
	filter2 := signedCharClampHigh(int(filter)+3, bd) >> 3

	*oq0 = uint16(signedCharClampHigh(int(qs0)-int(filter1), bd) + bias)
	*op0 = uint16(signedCharClampHigh(int(ps0)+int(filter2), bd) + bias)

	outer := int16((int(filter1)+1)>>1) & ^hev
	*oq1 = uint16(signedCharClampHigh(int(qs1)-int(outer), bd) + bias)
	*op1 = uint16(signedCharClampHigh(int(ps1)+int(outer), bd) + bias)
}

func highbdFilter8(mask int8, thresh uint8, flat int8,
	op3, op2, op1, op0, oq0, oq1, oq2, oq3 *uint16, bd int,
) {
	if flat != 0 && mask != 0 {
		p3, p2, p1, p0 := int(*op3), int(*op2), int(*op1), int(*op0)
		q0, q1, q2, q3 := int(*oq0), int(*oq1), int(*oq2), int(*oq3)
		*op2 = uint16((p3 + p3 + p3 + 2*p2 + p1 + p0 + q0 + 4) >> 3)
		*op1 = uint16((p3 + p3 + p2 + 2*p1 + p0 + q0 + q1 + 4) >> 3)
		*op0 = uint16((p3 + p2 + p1 + 2*p0 + q0 + q1 + q2 + 4) >> 3)
		*oq0 = uint16((p2 + p1 + p0 + 2*q0 + q1 + q2 + q3 + 4) >> 3)
		*oq1 = uint16((p1 + p0 + q0 + 2*q1 + q2 + q3 + q3 + 4) >> 3)
		*oq2 = uint16((p0 + q0 + q1 + 2*q2 + q3 + q3 + q3 + 4) >> 3)
	} else {
		highbdFilter4(mask, thresh, op1, op0, oq0, oq1, bd)
	}
}

func highbdFilter16(mask int8, thresh uint8, flat, flat2 int8,
	op7, op6, op5, op4, op3, op2, op1, op0,
	oq0, oq1, oq2, oq3, oq4, oq5, oq6, oq7 *uint16, bd int,
) {
	if flat2 != 0 && flat != 0 && mask != 0 {
		p7, p6, p5, p4 := int(*op7), int(*op6), int(*op5), int(*op4)
		p3, p2, p1, p0 := int(*op3), int(*op2), int(*op1), int(*op0)
		q0, q1, q2, q3 := int(*oq0), int(*oq1), int(*oq2), int(*oq3)
		q4, q5, q6, q7 := int(*oq4), int(*oq5), int(*oq6), int(*oq7)
		*op6 = uint16((p7*7 + p6*2 + p5 + p4 + p3 + p2 + p1 + p0 + q0 + 8) >> 4)
		*op5 = uint16((p7*6 + p6 + p5*2 + p4 + p3 + p2 + p1 + p0 + q0 + q1 + 8) >> 4)
		*op4 = uint16((p7*5 + p6 + p5 + p4*2 + p3 + p2 + p1 + p0 + q0 + q1 + q2 + 8) >> 4)
		*op3 = uint16((p7*4 + p6 + p5 + p4 + p3*2 + p2 + p1 + p0 + q0 + q1 + q2 + q3 + 8) >> 4)
		*op2 = uint16((p7*3 + p6 + p5 + p4 + p3 + p2*2 + p1 + p0 + q0 + q1 + q2 + q3 + q4 + 8) >> 4)
		*op1 = uint16((p7*2 + p6 + p5 + p4 + p3 + p2 + p1*2 + p0 + q0 + q1 + q2 + q3 + q4 + q5 + 8) >> 4)
		*op0 = uint16((p7 + p6 + p5 + p4 + p3 + p2 + p1 + p0*2 + q0 + q1 + q2 + q3 + q4 + q5 + q6 + 8) >> 4)
		*oq0 = uint16((p6 + p5 + p4 + p3 + p2 + p1 + p0 + q0*2 + q1 + q2 + q3 + q4 + q5 + q6 + q7 + 8) >> 4)
		*oq1 = uint16((p5 + p4 + p3 + p2 + p1 + p0 + q0 + q1*2 + q2 + q3 + q4 + q5 + q6 + q7*2 + 8) >> 4)
		*oq2 = uint16((p4 + p3 + p2 + p1 + p0 + q0 + q1 + q2*2 + q3 + q4 + q5 + q6 + q7*3 + 8) >> 4)
		*oq3 = uint16((p3 + p2 + p1 + p0 + q0 + q1 + q2 + q3*2 + q4 + q5 + q6 + q7*4 + 8) >> 4)
		*oq4 = uint16((p2 + p1 + p0 + q0 + q1 + q2 + q3 + q4*2 + q5 + q6 + q7*5 + 8) >> 4)
		*oq5 = uint16((p1 + p0 + q0 + q1 + q2 + q3 + q4 + q5*2 + q6 + q7*6 + 8) >> 4)
		*oq6 = uint16((p0 + q0 + q1 + q2 + q3 + q4 + q5 + q6*2 + q7*7 + 8) >> 4)
	} else {
		highbdFilter8(mask, thresh, flat, op3, op2, op1, op0, oq0, oq1, oq2, oq3, bd)
	}
}

func HighbdLpfHorizontal4(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	for i := range 8 {
		p := off + i
		p3, p2, p1, p0 := s[p-4*pitch], s[p-3*pitch], s[p-2*pitch], s[p-pitch]
		q0, q1, q2, q3 := s[p], s[p+pitch], s[p+2*pitch], s[p+3*pitch]
		mask := highbdFilterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		highbdFilter4(mask, thresh, &s[p-2*pitch], &s[p-pitch], &s[p], &s[p+pitch], bd)
	}
}

func HighbdLpfHorizontal4Dual(s []uint16, off, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8, bd int,
) {
	HighbdLpfHorizontal4(s, off, pitch, blimit0, limit0, thresh0, bd)
	HighbdLpfHorizontal4(s, off+8, pitch, blimit1, limit1, thresh1, bd)
}

func HighbdLpfVertical4(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	for i := range 8 {
		p := off + i*pitch
		p3, p2, p1, p0 := s[p-4], s[p-3], s[p-2], s[p-1]
		q0, q1, q2, q3 := s[p], s[p+1], s[p+2], s[p+3]
		mask := highbdFilterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		highbdFilter4(mask, thresh, &s[p-2], &s[p-1], &s[p], &s[p+1], bd)
	}
}

func HighbdLpfVertical4Dual(s []uint16, off, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8, bd int,
) {
	HighbdLpfVertical4(s, off, pitch, blimit0, limit0, thresh0, bd)
	HighbdLpfVertical4(s, off+8*pitch, pitch, blimit1, limit1, thresh1, bd)
}

func HighbdLpfHorizontal8(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	for i := range 8 {
		p := off + i
		p3, p2, p1, p0 := s[p-4*pitch], s[p-3*pitch], s[p-2*pitch], s[p-pitch]
		q0, q1, q2, q3 := s[p], s[p+pitch], s[p+2*pitch], s[p+3*pitch]
		mask := highbdFilterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		flat := highbdFlatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		highbdFilter8(mask, thresh, flat,
			&s[p-4*pitch], &s[p-3*pitch], &s[p-2*pitch], &s[p-pitch],
			&s[p], &s[p+pitch], &s[p+2*pitch], &s[p+3*pitch], bd)
	}
}

func HighbdLpfHorizontal8Dual(s []uint16, off, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8, bd int,
) {
	HighbdLpfHorizontal8(s, off, pitch, blimit0, limit0, thresh0, bd)
	HighbdLpfHorizontal8(s, off+8, pitch, blimit1, limit1, thresh1, bd)
}

func HighbdLpfVertical8(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	for i := range 8 {
		p := off + i*pitch
		p3, p2, p1, p0 := s[p-4], s[p-3], s[p-2], s[p-1]
		q0, q1, q2, q3 := s[p], s[p+1], s[p+2], s[p+3]
		mask := highbdFilterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		flat := highbdFlatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		highbdFilter8(mask, thresh, flat,
			&s[p-4], &s[p-3], &s[p-2], &s[p-1],
			&s[p], &s[p+1], &s[p+2], &s[p+3], bd)
	}
}

func HighbdLpfVertical8Dual(s []uint16, off, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8, bd int,
) {
	HighbdLpfVertical8(s, off, pitch, blimit0, limit0, thresh0, bd)
	HighbdLpfVertical8(s, off+8*pitch, pitch, blimit1, limit1, thresh1, bd)
}

func highbdMbLpfHorizontalEdgeW(s []uint16, off, pitch int,
	blimit, limit, thresh uint8, count, bd int,
) {
	for i := range 8 * count {
		p := off + i
		p3, p2, p1, p0 := s[p-4*pitch], s[p-3*pitch], s[p-2*pitch], s[p-pitch]
		q0, q1, q2, q3 := s[p], s[p+pitch], s[p+2*pitch], s[p+3*pitch]
		mask := highbdFilterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		flat := highbdFlatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		flat2 := highbdFlatMask5(1,
			s[p-8*pitch], s[p-7*pitch], s[p-6*pitch], s[p-5*pitch], p0, q0,
			s[p+4*pitch], s[p+5*pitch], s[p+6*pitch], s[p+7*pitch], bd)
		highbdFilter16(mask, thresh, flat, flat2,
			&s[p-8*pitch], &s[p-7*pitch], &s[p-6*pitch], &s[p-5*pitch],
			&s[p-4*pitch], &s[p-3*pitch], &s[p-2*pitch], &s[p-pitch],
			&s[p], &s[p+pitch], &s[p+2*pitch], &s[p+3*pitch],
			&s[p+4*pitch], &s[p+5*pitch], &s[p+6*pitch], &s[p+7*pitch], bd)
	}
}

func HighbdLpfHorizontal16(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	highbdMbLpfHorizontalEdgeW(s, off, pitch, blimit, limit, thresh, 1, bd)
}

func HighbdLpfHorizontal16Dual(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	highbdMbLpfHorizontalEdgeW(s, off, pitch, blimit, limit, thresh, 2, bd)
}

func highbdMbLpfVerticalEdgeW(s []uint16, off, pitch int,
	blimit, limit, thresh uint8, count, bd int,
) {
	for i := range count {
		p := off + i*pitch
		p3, p2, p1, p0 := s[p-4], s[p-3], s[p-2], s[p-1]
		q0, q1, q2, q3 := s[p], s[p+1], s[p+2], s[p+3]
		mask := highbdFilterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		flat := highbdFlatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3, bd)
		flat2 := highbdFlatMask5(1,
			s[p-8], s[p-7], s[p-6], s[p-5], p0, q0,
			s[p+4], s[p+5], s[p+6], s[p+7], bd)
		highbdFilter16(mask, thresh, flat, flat2,
			&s[p-8], &s[p-7], &s[p-6], &s[p-5],
			&s[p-4], &s[p-3], &s[p-2], &s[p-1],
			&s[p], &s[p+1], &s[p+2], &s[p+3],
			&s[p+4], &s[p+5], &s[p+6], &s[p+7], bd)
	}
}

func HighbdLpfVertical16(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	highbdMbLpfVerticalEdgeW(s, off, pitch, blimit, limit, thresh, 8, bd)
}

func HighbdLpfVertical16Dual(s []uint16, off, pitch int, blimit, limit, thresh uint8, bd int) {
	highbdMbLpfVerticalEdgeW(s, off, pitch, blimit, limit, thresh, 16, bd)
}

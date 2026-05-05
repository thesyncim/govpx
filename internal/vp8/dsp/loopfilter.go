package dsp

// Ported from libvpx v1.16.0 vp8/common/loopfilter_filters.c.

const MaxLoopFilter = 63

func LoopFilterSimpleHorizontalEdge(s []byte, stride int, blimit byte) {
	_ = s[3*stride+15]

	for i := 0; i < 16; i++ {
		q0 := 2*stride + i
		mask := simpleFilterMask(blimit, s[q0-2*stride], s[q0-stride], s[q0], s[q0+stride])
		simpleFilter(mask, &s[q0-2*stride], &s[q0-stride], &s[q0], &s[q0+stride])
	}
}

func LoopFilterSimpleVerticalEdge(s []byte, stride int, blimit byte) {
	_ = s[15*stride+3]

	for i := 0; i < 16; i++ {
		q0 := i*stride + 2
		mask := simpleFilterMask(blimit, s[q0-2], s[q0-1], s[q0], s[q0+1])
		simpleFilter(mask, &s[q0-2], &s[q0-1], &s[q0], &s[q0+1])
	}
}

func LoopFilterHorizontalEdge(s []byte, stride int, blimit byte, limit byte, thresh byte, count int) {
	_ = s[7*stride+count*8-1]

	for i := 0; i < count*8; i++ {
		q0 := 4*stride + i
		mask := filterMask(limit, blimit, s[q0-4*stride], s[q0-3*stride], s[q0-2*stride], s[q0-stride], s[q0], s[q0+stride], s[q0+2*stride], s[q0+3*stride])
		hev := hevMask(thresh, s[q0-2*stride], s[q0-stride], s[q0], s[q0+stride])
		loopFilter(mask, hev, &s[q0-2*stride], &s[q0-stride], &s[q0], &s[q0+stride])
	}
}

func LoopFilterVerticalEdge(s []byte, stride int, blimit byte, limit byte, thresh byte, count int) {
	_ = s[(count*8-1)*stride+7]

	for i := 0; i < count*8; i++ {
		q0 := i*stride + 4
		mask := filterMask(limit, blimit, s[q0-4], s[q0-3], s[q0-2], s[q0-1], s[q0], s[q0+1], s[q0+2], s[q0+3])
		hev := hevMask(thresh, s[q0-2], s[q0-1], s[q0], s[q0+1])
		loopFilter(mask, hev, &s[q0-2], &s[q0-1], &s[q0], &s[q0+1])
	}
}

func MBLoopFilterHorizontalEdge(s []byte, stride int, blimit byte, limit byte, thresh byte, count int) {
	_ = s[7*stride+count*8-1]

	for i := 0; i < count*8; i++ {
		q0 := 4*stride + i
		mask := filterMask(limit, blimit, s[q0-4*stride], s[q0-3*stride], s[q0-2*stride], s[q0-stride], s[q0], s[q0+stride], s[q0+2*stride], s[q0+3*stride])
		hev := hevMask(thresh, s[q0-2*stride], s[q0-stride], s[q0], s[q0+stride])
		mbLoopFilter(mask, hev, &s[q0-3*stride], &s[q0-2*stride], &s[q0-stride], &s[q0], &s[q0+stride], &s[q0+2*stride])
	}
}

func MBLoopFilterVerticalEdge(s []byte, stride int, blimit byte, limit byte, thresh byte, count int) {
	_ = s[(count*8-1)*stride+7]

	for i := 0; i < count*8; i++ {
		q0 := i*stride + 4
		mask := filterMask(limit, blimit, s[q0-4], s[q0-3], s[q0-2], s[q0-1], s[q0], s[q0+1], s[q0+2], s[q0+3])
		hev := hevMask(thresh, s[q0-2], s[q0-1], s[q0], s[q0+1])
		mbLoopFilter(mask, hev, &s[q0-3], &s[q0-2], &s[q0-1], &s[q0], &s[q0+1], &s[q0+2])
	}
}

func signedCharClamp(v int) int8 {
	if v < -128 {
		return -128
	}
	if v > 127 {
		return 127
	}
	return int8(v)
}

func filterMask(limit byte, blimit byte, p3 byte, p2 byte, p1 byte, p0 byte, q0 byte, q1 byte, q2 byte, q3 byte) int8 {
	mask := byte(0)
	if absByteDiff(p3, p2) > limit {
		mask = 1
	}
	if absByteDiff(p2, p1) > limit {
		mask = 1
	}
	if absByteDiff(p1, p0) > limit {
		mask = 1
	}
	if absByteDiff(q1, q0) > limit {
		mask = 1
	}
	if absByteDiff(q2, q1) > limit {
		mask = 1
	}
	if absByteDiff(q3, q2) > limit {
		mask = 1
	}
	if int(absByteDiff(p0, q0))*2+int(absByteDiff(p1, q1))/2 > int(blimit) {
		mask = 1
	}
	return int8(mask) - 1
}

func hevMask(thresh byte, p1 byte, p0 byte, q0 byte, q1 byte) int8 {
	hev := int8(0)
	if absByteDiff(p1, p0) > thresh {
		hev = -1
	}
	if absByteDiff(q1, q0) > thresh {
		hev = -1
	}
	return hev
}

func loopFilter(mask int8, hev int8, op1 *byte, op0 *byte, oq0 *byte, oq1 *byte) {
	ps1 := signedPixel(*op1)
	ps0 := signedPixel(*op0)
	qs0 := signedPixel(*oq0)
	qs1 := signedPixel(*oq1)

	filterValue := signedCharClamp(int(ps1) - int(qs1))
	filterValue &= hev
	filterValue = signedCharClamp(int(filterValue) + 3*(int(qs0)-int(ps0)))
	filterValue &= mask

	filter1 := signedCharClamp(int(filterValue) + 4)
	filter2 := signedCharClamp(int(filterValue) + 3)
	filter1 = int8(int(filter1) >> 3)
	filter2 = int8(int(filter2) >> 3)
	*oq0 = unsignedPixel(signedCharClamp(int(qs0) - int(filter1)))
	*op0 = unsignedPixel(signedCharClamp(int(ps0) + int(filter2)))

	filterValue = filter1
	filterValue++
	filterValue = int8(int(filterValue) >> 1)
	filterValue &= ^hev

	*oq1 = unsignedPixel(signedCharClamp(int(qs1) - int(filterValue)))
	*op1 = unsignedPixel(signedCharClamp(int(ps1) + int(filterValue)))
}

func mbLoopFilter(mask int8, hev int8, op2 *byte, op1 *byte, op0 *byte, oq0 *byte, oq1 *byte, oq2 *byte) {
	ps2 := signedPixel(*op2)
	ps1 := signedPixel(*op1)
	ps0 := signedPixel(*op0)
	qs0 := signedPixel(*oq0)
	qs1 := signedPixel(*oq1)
	qs2 := signedPixel(*oq2)

	filterValue := signedCharClamp(int(ps1) - int(qs1))
	filterValue = signedCharClamp(int(filterValue) + 3*(int(qs0)-int(ps0)))
	filterValue &= mask

	filter2 := filterValue & hev
	filter1 := signedCharClamp(int(filter2) + 4)
	filter2 = signedCharClamp(int(filter2) + 3)
	filter1 = int8(int(filter1) >> 3)
	filter2 = int8(int(filter2) >> 3)
	qs0 = signedCharClamp(int(qs0) - int(filter1))
	ps0 = signedCharClamp(int(ps0) + int(filter2))

	filterValue &= ^hev
	filter2 = filterValue

	u := signedCharClamp((63 + int(filter2)*27) >> 7)
	*oq0 = unsignedPixel(signedCharClamp(int(qs0) - int(u)))
	*op0 = unsignedPixel(signedCharClamp(int(ps0) + int(u)))

	u = signedCharClamp((63 + int(filter2)*18) >> 7)
	*oq1 = unsignedPixel(signedCharClamp(int(qs1) - int(u)))
	*op1 = unsignedPixel(signedCharClamp(int(ps1) + int(u)))

	u = signedCharClamp((63 + int(filter2)*9) >> 7)
	*oq2 = unsignedPixel(signedCharClamp(int(qs2) - int(u)))
	*op2 = unsignedPixel(signedCharClamp(int(ps2) + int(u)))
}

func simpleFilterMask(blimit byte, p1 byte, p0 byte, q0 byte, q1 byte) int8 {
	if int(absByteDiff(p0, q0))*2+int(absByteDiff(p1, q1))/2 <= int(blimit) {
		return -1
	}
	return 0
}

func simpleFilter(mask int8, op1 *byte, op0 *byte, oq0 *byte, oq1 *byte) {
	p1 := signedPixel(*op1)
	p0 := signedPixel(*op0)
	q0 := signedPixel(*oq0)
	q1 := signedPixel(*oq1)

	filterValue := signedCharClamp(int(p1) - int(q1))
	filterValue = signedCharClamp(int(filterValue) + 3*(int(q0)-int(p0)))
	filterValue &= mask

	filter1 := signedCharClamp(int(filterValue) + 4)
	filter1 = int8(int(filter1) >> 3)
	*oq0 = unsignedPixel(signedCharClamp(int(q0) - int(filter1)))

	filter2 := signedCharClamp(int(filterValue) + 3)
	filter2 = int8(int(filter2) >> 3)
	*op0 = unsignedPixel(signedCharClamp(int(p0) + int(filter2)))
}

func signedPixel(v byte) int8 {
	return int8(v ^ 0x80)
}

func unsignedPixel(v int8) byte {
	return byte(v) ^ 0x80
}

func absByteDiff(a byte, b byte) byte {
	if a > b {
		return a - b
	}
	return b - a
}

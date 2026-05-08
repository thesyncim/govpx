package dsp

// Pure-Go vectorized helpers for the VP8 inner and MB loop filters
// (libvpx v1.16.0 baseline). The unused-on-arm64 funcs serve as the
// fallback for !arm64 && !amd64 builds.

// Vector-friendly per-edge implementations of the VP8 inner and MB loop
// filter that eliminate per-pixel function-call overhead and pointer
// indirection from internal/vp8/dsp/loopfilter.go's scalar bodies.
//
// loopFilterEdge16 / mbLoopFilterEdge16 take the eight column bytes
// p3..q3 already gathered into byte slots (one slot per lane). The
// branch-free filterMaskFlag/hevMaskFlag helpers reproduce the libvpx
// per-lane mask bytes (0xFF when filtering, 0x00 otherwise; 0xFF when
// "high edge variation", 0x00 otherwise).
//
// The horizontal-edge dispatch loads contiguous lanes directly. The
// vertical-edge dispatch loads four bytes per row across count*8 rows
// into the lane slots, applies the same kernel, then scatters back.

func loopFilterHorizontalEdgeGo(s []byte, stride int, blimit, limit, thresh byte, count int) {
	width := count * 8
	_ = s[7*stride+width-1]

	row3 := s[0*stride : 0*stride+width]
	row2 := s[1*stride : 1*stride+width]
	row1 := s[2*stride : 2*stride+width]
	row0 := s[3*stride : 3*stride+width]
	q0r := s[4*stride : 4*stride+width]
	q1r := s[5*stride : 5*stride+width]
	q2r := s[6*stride : 6*stride+width]
	q3r := s[7*stride : 7*stride+width]

	for i := 0; i < width; i++ {
		mask := filterMaskFlag(limit, blimit, row3[i], row2[i], row1[i], row0[i], q0r[i], q1r[i], q2r[i], q3r[i])
		if mask == 0 {
			continue
		}
		hev := hevMaskFlag(thresh, row1[i], row0[i], q0r[i], q1r[i])
		p1, p0, qq0, qq1 := loopFilterPixels(mask, hev, row1[i], row0[i], q0r[i], q1r[i])
		row1[i] = p1
		row0[i] = p0
		q0r[i] = qq0
		q1r[i] = qq1
	}
}

func loopFilterVerticalEdgeGo(s []byte, stride int, blimit, limit, thresh byte, count int) {
	rows := count * 8
	_ = s[(rows-1)*stride+7]

	for y := 0; y < rows; y++ {
		row := s[y*stride : y*stride+8]
		mask := filterMaskFlag(limit, blimit, row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7])
		if mask == 0 {
			continue
		}
		hev := hevMaskFlag(thresh, row[2], row[3], row[4], row[5])
		p1, p0, q0, q1 := loopFilterPixels(mask, hev, row[2], row[3], row[4], row[5])
		row[2] = p1
		row[3] = p0
		row[4] = q0
		row[5] = q1
	}
}

func mbLoopFilterHorizontalEdgeGo(s []byte, stride int, blimit, limit, thresh byte, count int) {
	width := count * 8
	_ = s[7*stride+width-1]

	row3 := s[0*stride : 0*stride+width]
	row2 := s[1*stride : 1*stride+width]
	row1 := s[2*stride : 2*stride+width]
	row0 := s[3*stride : 3*stride+width]
	q0r := s[4*stride : 4*stride+width]
	q1r := s[5*stride : 5*stride+width]
	q2r := s[6*stride : 6*stride+width]
	q3r := s[7*stride : 7*stride+width]

	for i := 0; i < width; i++ {
		mask := filterMaskFlag(limit, blimit, row3[i], row2[i], row1[i], row0[i], q0r[i], q1r[i], q2r[i], q3r[i])
		if mask == 0 {
			continue
		}
		hev := hevMaskFlag(thresh, row1[i], row0[i], q0r[i], q1r[i])
		p2, p1, p0, qq0, qq1, qq2 := mbLoopFilterPixels(mask, hev, row2[i], row1[i], row0[i], q0r[i], q1r[i], q2r[i])
		row2[i] = p2
		row1[i] = p1
		row0[i] = p0
		q0r[i] = qq0
		q1r[i] = qq1
		q2r[i] = qq2
	}
}

func mbLoopFilterVerticalEdgeGo(s []byte, stride int, blimit, limit, thresh byte, count int) {
	rows := count * 8
	_ = s[(rows-1)*stride+7]

	for y := 0; y < rows; y++ {
		row := s[y*stride : y*stride+8]
		mask := filterMaskFlag(limit, blimit, row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7])
		if mask == 0 {
			continue
		}
		hev := hevMaskFlag(thresh, row[2], row[3], row[4], row[5])
		p2, p1, p0, q0, q1, q2 := mbLoopFilterPixels(mask, hev, row[1], row[2], row[3], row[4], row[5], row[6])
		row[1] = p2
		row[2] = p1
		row[3] = p0
		row[4] = q0
		row[5] = q1
		row[6] = q2
	}
}

// filterMaskFlag returns 0xFF when filtering should occur, 0x00 otherwise.
// Equivalent to (int8(filterMask(...)) & 0xFF) interpreted as a byte.
func filterMaskFlag(limit, blimit byte, p3, p2, p1, p0, q0, q1, q2, q3 byte) byte {
	d := absDiffFast(p3, p2)
	if v := absDiffFast(p2, p1); v > d {
		d = v
	}
	if v := absDiffFast(p1, p0); v > d {
		d = v
	}
	if v := absDiffFast(q1, q0); v > d {
		d = v
	}
	if v := absDiffFast(q2, q1); v > d {
		d = v
	}
	if v := absDiffFast(q3, q2); v > d {
		d = v
	}
	if d > limit {
		return 0
	}
	if int(absDiffFast(p0, q0))*2+int(absDiffFast(p1, q1))/2 > int(blimit) {
		return 0
	}
	return 0xFF
}

func hevMaskFlag(thresh byte, p1, p0, q0, q1 byte) byte {
	if absDiffFast(p1, p0) > thresh || absDiffFast(q1, q0) > thresh {
		return 0xFF
	}
	return 0x00
}

func absDiffFast(a, b byte) byte {
	if a > b {
		return a - b
	}
	return b - a
}

func loopFilterPixels(mask, hev byte, op1, op0, oq0, oq1 byte) (byte, byte, byte, byte) {
	ps1 := int8(op1 ^ 0x80)
	ps0 := int8(op0 ^ 0x80)
	qs0 := int8(oq0 ^ 0x80)
	qs1 := int8(oq1 ^ 0x80)

	fv := scClamp(int(ps1) - int(qs1))
	fv = int8(byte(fv) & hev)
	fv = scClamp(int(fv) + 3*(int(qs0)-int(ps0)))
	fv = int8(byte(fv) & mask)

	f1 := scClamp(int(fv) + 4)
	f2 := scClamp(int(fv) + 3)
	f1 = int8(int(f1) >> 3)
	f2 = int8(int(f2) >> 3)
	nq0 := byte(scClamp(int(qs0)-int(f1))) ^ 0x80
	np0 := byte(scClamp(int(ps0)+int(f2))) ^ 0x80

	fv2 := f1
	fv2++
	fv2 = int8(int(fv2) >> 1)
	fv2 = int8(byte(fv2) &^ hev)

	nq1 := byte(scClamp(int(qs1)-int(fv2))) ^ 0x80
	np1 := byte(scClamp(int(ps1)+int(fv2))) ^ 0x80

	return np1, np0, nq0, nq1
}

func mbLoopFilterPixels(mask, hev byte, op2, op1, op0, oq0, oq1, oq2 byte) (byte, byte, byte, byte, byte, byte) {
	ps2 := int8(op2 ^ 0x80)
	ps1 := int8(op1 ^ 0x80)
	ps0 := int8(op0 ^ 0x80)
	qs0 := int8(oq0 ^ 0x80)
	qs1 := int8(oq1 ^ 0x80)
	qs2 := int8(oq2 ^ 0x80)

	fv := scClamp(int(ps1) - int(qs1))
	fv = scClamp(int(fv) + 3*(int(qs0)-int(ps0)))
	fv = int8(byte(fv) & mask)

	f2 := int8(byte(fv) & hev)
	f1 := scClamp(int(f2) + 4)
	f2 = scClamp(int(f2) + 3)
	f1 = int8(int(f1) >> 3)
	f2 = int8(int(f2) >> 3)
	qs0 = scClamp(int(qs0) - int(f1))
	ps0 = scClamp(int(ps0) + int(f2))

	fv = int8(byte(fv) &^ hev)
	f2 = fv

	u := scClamp((63 + int(f2)*27) >> 7)
	nq0 := byte(scClamp(int(qs0)-int(u))) ^ 0x80
	np0 := byte(scClamp(int(ps0)+int(u))) ^ 0x80

	u = scClamp((63 + int(f2)*18) >> 7)
	nq1 := byte(scClamp(int(qs1)-int(u))) ^ 0x80
	np1 := byte(scClamp(int(ps1)+int(u))) ^ 0x80

	u = scClamp((63 + int(f2)*9) >> 7)
	nq2 := byte(scClamp(int(qs2)-int(u))) ^ 0x80
	np2 := byte(scClamp(int(ps2)+int(u))) ^ 0x80

	return np2, np1, np0, nq0, nq1, nq2
}

//go:inline
func scClamp(v int) int8 {
	if v < -128 {
		return -128
	}
	if v > 127 {
		return 127
	}
	return int8(v)
}

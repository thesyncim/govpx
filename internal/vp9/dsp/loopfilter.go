package dsp

// VP9 in-loop deblocking filter primitives. Ported from libvpx v1.16.0
// vpx_dsp/loopfilter.c. The 4-pixel filter handles the narrowest edge
// step (one MB boundary inside an 8x8); the 8 and 16-pixel variants
// extend the active set and add the "flat" cases. _dual wrappers run
// two segments at independent thresholds and just compose the base
// kernel twice; they are not re-ported separately.

// signedCharClamp models libvpx's signed_char_clamp helper — clamp into
// the int8 range so the subsequent shift-by-3 acts predictably on the
// sign bits.
func signedCharClamp(t int) int8 {
	if t < -128 {
		return -128
	}
	if t > 127 {
		return 127
	}
	return int8(t)
}

func absDiff(a, b uint8) int {
	d := int(a) - int(b)
	mask := d >> 31
	return (d ^ mask) - mask
}

// filterMask returns 0xff if the 8-pixel edge needs filtering, 0
// otherwise. Bit-identical to filter_mask in vpx_dsp/loopfilter.c.
func filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3 uint8) int8 {
	var mask int8
	if absDiff(p3, p2) > int(limit) {
		mask |= -1
	}
	if absDiff(p2, p1) > int(limit) {
		mask |= -1
	}
	if absDiff(p1, p0) > int(limit) {
		mask |= -1
	}
	if absDiff(q1, q0) > int(limit) {
		mask |= -1
	}
	if absDiff(q2, q1) > int(limit) {
		mask |= -1
	}
	if absDiff(q3, q2) > int(limit) {
		mask |= -1
	}
	if absDiff(p0, q0)*2+absDiff(p1, q1)/2 > int(blimit) {
		mask |= -1
	}
	return ^mask
}

// flatMask4 returns 0xff if the 8-pixel edge is "flat" enough to swap
// in the 7-tap filter, 0 otherwise. Bit-identical to flat_mask4.
func flatMask4(thresh, p3, p2, p1, p0, q0, q1, q2, q3 uint8) int8 {
	var mask int8
	if absDiff(p1, p0) > int(thresh) {
		mask |= -1
	}
	if absDiff(q1, q0) > int(thresh) {
		mask |= -1
	}
	if absDiff(p2, p0) > int(thresh) {
		mask |= -1
	}
	if absDiff(q2, q0) > int(thresh) {
		mask |= -1
	}
	if absDiff(p3, p0) > int(thresh) {
		mask |= -1
	}
	if absDiff(q3, q0) > int(thresh) {
		mask |= -1
	}
	return ^mask
}

// hevMask returns 0xff if the inner-edge variance is high, signalling
// the filter should also adjust the outer taps. Bit-identical to
// hev_mask.
func hevMask(thresh, p1, p0, q0, q1 uint8) int8 {
	var hev int8
	if absDiff(p1, p0) > int(thresh) {
		hev |= -1
	}
	if absDiff(q1, q0) > int(thresh) {
		hev |= -1
	}
	return hev
}

// filter4 is the 4-tap edge filter. Operates in-place on four caller-
// owned pixel slots (p1, p0, q0, q1) referenced by index. Bit-identical
// to filter4 in vpx_dsp/loopfilter.c.
func filter4(mask int8, thresh uint8, dst []uint8, idxP1, idxP0, idxQ0, idxQ1 int) {
	op1, op0, oq0, oq1 := dst[idxP1], dst[idxP0], dst[idxQ0], dst[idxQ1]
	ps1 := int8(op1 ^ 0x80)
	ps0 := int8(op0 ^ 0x80)
	qs0 := int8(oq0 ^ 0x80)
	qs1 := int8(oq1 ^ 0x80)
	hev := hevMask(thresh, op1, op0, oq0, oq1)

	filter := signedCharClamp(int(ps1)-int(qs1)) & hev
	filter = signedCharClamp(int(filter)+3*(int(qs0)-int(ps0))) & mask
	filter1 := signedCharClamp(int(filter)+4) >> 3
	filter2 := signedCharClamp(int(filter)+3) >> 3

	dst[idxQ0] = uint8(signedCharClamp(int(qs0)-int(filter1))) ^ 0x80
	dst[idxP0] = uint8(signedCharClamp(int(ps0)+int(filter2))) ^ 0x80

	outer := (filter1 + 1) >> 1
	outer &= ^hev

	dst[idxQ1] = uint8(signedCharClamp(int(qs1)-int(outer))) ^ 0x80
	dst[idxP1] = uint8(signedCharClamp(int(ps1)+int(outer))) ^ 0x80
}

func filter4Pass(thresh uint8, dst []uint8, idxP1, idxP0, idxQ0, idxQ1 int, op1, op0, oq0, oq1 uint8) {
	ps1 := int8(op1 ^ 0x80)
	ps0 := int8(op0 ^ 0x80)
	qs0 := int8(oq0 ^ 0x80)
	qs1 := int8(oq1 ^ 0x80)
	hev := hevMask(thresh, op1, op0, oq0, oq1)

	filter := signedCharClamp(int(ps1)-int(qs1)) & hev
	filter = signedCharClamp(int(filter) + 3*(int(qs0)-int(ps0)))
	filter1 := signedCharClamp(int(filter)+4) >> 3
	filter2 := signedCharClamp(int(filter)+3) >> 3

	dst[idxQ0] = uint8(signedCharClamp(int(qs0)-int(filter1))) ^ 0x80
	dst[idxP0] = uint8(signedCharClamp(int(ps0)+int(filter2))) ^ 0x80

	outer := (filter1 + 1) >> 1
	outer &= ^hev

	dst[idxQ1] = uint8(signedCharClamp(int(qs1)-int(outer))) ^ 0x80
	dst[idxP1] = uint8(signedCharClamp(int(ps1)+int(outer))) ^ 0x80
}

// VpxLpfHorizontal4 mirrors vpx_lpf_horizontal_4_c. It filters 8
// horizontally-adjacent edge columns above and below the cursor pixel
// `s` (interpreted as offset within `plane`), each spanning 4 rows
// above and 4 below.
func VpxLpfHorizontal4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfHorizontal4(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal4Scalar(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for range 8 {
		p3 := plane[s-4*pitch]
		p2 := plane[s-3*pitch]
		p1 := plane[s-2*pitch]
		p0 := plane[s-pitch]
		q0 := plane[s+0]
		q1 := plane[s+1*pitch]
		q2 := plane[s+2*pitch]
		q3 := plane[s+3*pitch]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		if mask != 0 {
			filter4(mask, thresh, plane, s-2*pitch, s-pitch, s, s+pitch)
		}
		s++
	}
}

// VpxLpfVertical4 mirrors vpx_lpf_vertical_4_c. It filters 8
// vertically-adjacent edge rows left and right of the cursor pixel `s`,
// each spanning 4 columns left and 4 right.
func VpxLpfVertical4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfVertical4(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical4Scalar(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for range 8 {
		p3 := plane[s-4]
		p2 := plane[s-3]
		p1 := plane[s-2]
		p0 := plane[s-1]
		q0 := plane[s+0]
		q1 := plane[s+1]
		q2 := plane[s+2]
		q3 := plane[s+3]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		if mask != 0 {
			filter4(mask, thresh, plane, s-2, s-1, s, s+1)
		}
		s += pitch
	}
}

// VpxLpfHorizontal4Dual is vpx_lpf_horizontal_4_dual_c — filter the 8
// columns at `s` with the first threshold triplet, then the next 8 at
// `s+8` with the second.
func VpxLpfHorizontal4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfHorizontal4Dual(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal4DualScalar(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfHorizontal4Scalar(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfHorizontal4Scalar(plane, s+8, pitch, blimit1, limit1, thresh1)
}

// VpxLpfVertical4Dual is vpx_lpf_vertical_4_dual_c — filter the 8 rows
// at `s` with one threshold triplet, then the 8 below at `s+8*pitch`
// with the second.
func VpxLpfVertical4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfVertical4Dual(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical4DualScalar(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfVertical4Scalar(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfVertical4Scalar(plane, s+8*pitch, pitch, blimit1, limit1, thresh1)
}

// filter8 is the 7-tap edge filter applied when the edge meets the
// flat-mask4 criterion. Falls back to filter4 when the criterion fails.
// Operates in-place on eight caller-owned pixel slots via index lookups.
// Bit-identical to filter8 in vpx_dsp/loopfilter.c.
func filter8(mask int8, thresh uint8, flat int8, dst []uint8,
	idxP3, idxP2, idxP1, idxP0, idxQ0, idxQ1, idxQ2, idxQ3 int,
) {
	if flat != 0 && mask != 0 {
		p3, p2, p1, p0 := int(dst[idxP3]), int(dst[idxP2]), int(dst[idxP1]), int(dst[idxP0])
		q0, q1, q2, q3 := int(dst[idxQ0]), int(dst[idxQ1]), int(dst[idxQ2]), int(dst[idxQ3])
		dst[idxP2] = uint8((p3 + p3 + p3 + 2*p2 + p1 + p0 + q0 + 4) >> 3)
		dst[idxP1] = uint8((p3 + p3 + p2 + 2*p1 + p0 + q0 + q1 + 4) >> 3)
		dst[idxP0] = uint8((p3 + p2 + p1 + 2*p0 + q0 + q1 + q2 + 4) >> 3)
		dst[idxQ0] = uint8((p2 + p1 + p0 + 2*q0 + q1 + q2 + q3 + 4) >> 3)
		dst[idxQ1] = uint8((p1 + p0 + q0 + 2*q1 + q2 + q3 + q3 + 4) >> 3)
		dst[idxQ2] = uint8((p0 + q0 + q1 + 2*q2 + q3 + q3 + q3 + 4) >> 3)
		return
	}
	filter4(mask, thresh, dst, idxP1, idxP0, idxQ0, idxQ1)
}

// flatMask5 extends flat_mask4 to the wider 16-pixel filter footprint.
// Bit-identical to flat_mask5 in vpx_dsp/loopfilter.c.
func flatMask5(thresh, p4, p3, p2, p1, p0, q0, q1, q2, q3, q4 uint8) int8 {
	mask := ^flatMask4(thresh, p3, p2, p1, p0, q0, q1, q2, q3)
	if absDiff(p4, p0) > int(thresh) {
		mask |= -1
	}
	if absDiff(q4, q0) > int(thresh) {
		mask |= -1
	}
	return ^mask
}

// filter16 is the 15-tap edge filter applied when both flat_mask4 and
// flat_mask5 pass. Falls back to filter8 otherwise. Operates in-place
// on sixteen caller-owned pixel slots via index lookups.
// Bit-identical to filter16 in vpx_dsp/loopfilter.c.
func filter16(mask int8, thresh uint8, flat, flat2 int8, dst []uint8,
	idxP7, idxP6, idxP5, idxP4, idxP3, idxP2, idxP1, idxP0,
	idxQ0, idxQ1, idxQ2, idxQ3, idxQ4, idxQ5, idxQ6, idxQ7 int,
) {
	if flat2 != 0 && flat != 0 && mask != 0 {
		p7, p6, p5, p4 := int(dst[idxP7]), int(dst[idxP6]), int(dst[idxP5]), int(dst[idxP4])
		p3, p2, p1, p0 := int(dst[idxP3]), int(dst[idxP2]), int(dst[idxP1]), int(dst[idxP0])
		q0, q1, q2, q3 := int(dst[idxQ0]), int(dst[idxQ1]), int(dst[idxQ2]), int(dst[idxQ3])
		q4, q5, q6, q7 := int(dst[idxQ4]), int(dst[idxQ5]), int(dst[idxQ6]), int(dst[idxQ7])
		dst[idxP6] = uint8((p7*7 + p6*2 + p5 + p4 + p3 + p2 + p1 + p0 + q0 + 8) >> 4)
		dst[idxP5] = uint8((p7*6 + p6 + p5*2 + p4 + p3 + p2 + p1 + p0 + q0 + q1 + 8) >> 4)
		dst[idxP4] = uint8((p7*5 + p6 + p5 + p4*2 + p3 + p2 + p1 + p0 + q0 + q1 + q2 + 8) >> 4)
		dst[idxP3] = uint8((p7*4 + p6 + p5 + p4 + p3*2 + p2 + p1 + p0 + q0 + q1 + q2 + q3 + 8) >> 4)
		dst[idxP2] = uint8((p7*3 + p6 + p5 + p4 + p3 + p2*2 + p1 + p0 + q0 + q1 + q2 + q3 + q4 + 8) >> 4)
		dst[idxP1] = uint8((p7*2 + p6 + p5 + p4 + p3 + p2 + p1*2 + p0 + q0 + q1 + q2 + q3 + q4 + q5 + 8) >> 4)
		dst[idxP0] = uint8((p7 + p6 + p5 + p4 + p3 + p2 + p1 + p0*2 + q0 + q1 + q2 + q3 + q4 + q5 + q6 + 8) >> 4)
		dst[idxQ0] = uint8((p6 + p5 + p4 + p3 + p2 + p1 + p0 + q0*2 + q1 + q2 + q3 + q4 + q5 + q6 + q7 + 8) >> 4)
		dst[idxQ1] = uint8((p5 + p4 + p3 + p2 + p1 + p0 + q0 + q1*2 + q2 + q3 + q4 + q5 + q6 + q7*2 + 8) >> 4)
		dst[idxQ2] = uint8((p4 + p3 + p2 + p1 + p0 + q0 + q1 + q2*2 + q3 + q4 + q5 + q6 + q7*3 + 8) >> 4)
		dst[idxQ3] = uint8((p3 + p2 + p1 + p0 + q0 + q1 + q2 + q3*2 + q4 + q5 + q6 + q7*4 + 8) >> 4)
		dst[idxQ4] = uint8((p2 + p1 + p0 + q0 + q1 + q2 + q3 + q4*2 + q5 + q6 + q7*5 + 8) >> 4)
		dst[idxQ5] = uint8((p1 + p0 + q0 + q1 + q2 + q3 + q4 + q5*2 + q6 + q7*6 + 8) >> 4)
		dst[idxQ6] = uint8((p0 + q0 + q1 + q2 + q3 + q4 + q5 + q6*2 + q7*7 + 8) >> 4)
		return
	}
	filter8(mask, thresh, flat, dst, idxP3, idxP2, idxP1, idxP0, idxQ0, idxQ1, idxQ2, idxQ3)
}

// VpxLpfHorizontal8 mirrors vpx_lpf_horizontal_8_c.
func VpxLpfHorizontal8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfHorizontal8(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal8Scalar(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for range 8 {
		p3 := plane[s-4*pitch]
		p2 := plane[s-3*pitch]
		p1 := plane[s-2*pitch]
		p0 := plane[s-pitch]
		q0 := plane[s+0]
		q1 := plane[s+1*pitch]
		q2 := plane[s+2*pitch]
		q3 := plane[s+3*pitch]
		if absDiff(p3, p2) > int(limit) ||
			absDiff(p2, p1) > int(limit) ||
			absDiff(p1, p0) > int(limit) ||
			absDiff(q1, q0) > int(limit) ||
			absDiff(q2, q1) > int(limit) ||
			absDiff(q3, q2) > int(limit) ||
			absDiff(p0, q0)*2+absDiff(p1, q1)/2 > int(blimit) {
			s++
			continue
		}
		if absDiff(p1, p0) <= 1 &&
			absDiff(q1, q0) <= 1 &&
			absDiff(p2, p0) <= 1 &&
			absDiff(q2, q0) <= 1 &&
			absDiff(p3, p0) <= 1 &&
			absDiff(q3, q0) <= 1 {
			plane[s-3*pitch] = uint8((int(p3)*3 + int(p2)*2 + int(p1) + int(p0) + int(q0) + 4) >> 3)
			plane[s-2*pitch] = uint8((int(p3)*2 + int(p2) + int(p1)*2 + int(p0) + int(q0) + int(q1) + 4) >> 3)
			plane[s-pitch] = uint8((int(p3) + int(p2) + int(p1) + int(p0)*2 + int(q0) + int(q1) + int(q2) + 4) >> 3)
			plane[s] = uint8((int(p2) + int(p1) + int(p0) + int(q0)*2 + int(q1) + int(q2) + int(q3) + 4) >> 3)
			plane[s+pitch] = uint8((int(p1) + int(p0) + int(q0) + int(q1)*2 + int(q2) + int(q3)*2 + 4) >> 3)
			plane[s+2*pitch] = uint8((int(p0) + int(q0) + int(q1) + int(q2)*2 + int(q3)*3 + 4) >> 3)
		} else {
			ps1 := int8(p1 ^ 0x80)
			ps0 := int8(p0 ^ 0x80)
			qs0 := int8(q0 ^ 0x80)
			qs1 := int8(q1 ^ 0x80)
			hev := hevMask(thresh, p1, p0, q0, q1)

			filter := signedCharClamp(int(ps1)-int(qs1)) & hev
			filter = signedCharClamp(int(filter) + 3*(int(qs0)-int(ps0)))
			filter1 := signedCharClamp(int(filter)+4) >> 3
			filter2 := signedCharClamp(int(filter)+3) >> 3

			plane[s] = uint8(signedCharClamp(int(qs0)-int(filter1))) ^ 0x80
			plane[s-pitch] = uint8(signedCharClamp(int(ps0)+int(filter2))) ^ 0x80

			outer := (filter1 + 1) >> 1
			outer &= ^hev

			plane[s+pitch] = uint8(signedCharClamp(int(qs1)-int(outer))) ^ 0x80
			plane[s-2*pitch] = uint8(signedCharClamp(int(ps1)+int(outer))) ^ 0x80
		}
		s++
	}
}

// VpxLpfVertical8 mirrors vpx_lpf_vertical_8_c.
func VpxLpfVertical8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	vpxLpfVertical8(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical8Scalar(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for range 8 {
		p3 := plane[s-4]
		p2 := plane[s-3]
		p1 := plane[s-2]
		p0 := plane[s-1]
		q0 := plane[s+0]
		q1 := plane[s+1]
		q2 := plane[s+2]
		q3 := plane[s+3]
		if absDiff(p3, p2) > int(limit) ||
			absDiff(p2, p1) > int(limit) ||
			absDiff(p1, p0) > int(limit) ||
			absDiff(q1, q0) > int(limit) ||
			absDiff(q2, q1) > int(limit) ||
			absDiff(q3, q2) > int(limit) ||
			absDiff(p0, q0)*2+absDiff(p1, q1)/2 > int(blimit) {
			s += pitch
			continue
		}
		if absDiff(p1, p0) <= 1 &&
			absDiff(q1, q0) <= 1 &&
			absDiff(p2, p0) <= 1 &&
			absDiff(q2, q0) <= 1 &&
			absDiff(p3, p0) <= 1 &&
			absDiff(q3, q0) <= 1 {
			plane[s-3] = uint8((int(p3)*3 + int(p2)*2 + int(p1) + int(p0) + int(q0) + 4) >> 3)
			plane[s-2] = uint8((int(p3)*2 + int(p2) + int(p1)*2 + int(p0) + int(q0) + int(q1) + 4) >> 3)
			plane[s-1] = uint8((int(p3) + int(p2) + int(p1) + int(p0)*2 + int(q0) + int(q1) + int(q2) + 4) >> 3)
			plane[s] = uint8((int(p2) + int(p1) + int(p0) + int(q0)*2 + int(q1) + int(q2) + int(q3) + 4) >> 3)
			plane[s+1] = uint8((int(p1) + int(p0) + int(q0) + int(q1)*2 + int(q2) + int(q3)*2 + 4) >> 3)
			plane[s+2] = uint8((int(p0) + int(q0) + int(q1) + int(q2)*2 + int(q3)*3 + 4) >> 3)
		} else {
			ps1 := int8(p1 ^ 0x80)
			ps0 := int8(p0 ^ 0x80)
			qs0 := int8(q0 ^ 0x80)
			qs1 := int8(q1 ^ 0x80)
			hev := hevMask(thresh, p1, p0, q0, q1)

			filter := signedCharClamp(int(ps1)-int(qs1)) & hev
			filter = signedCharClamp(int(filter) + 3*(int(qs0)-int(ps0)))
			filter1 := signedCharClamp(int(filter)+4) >> 3
			filter2 := signedCharClamp(int(filter)+3) >> 3

			plane[s] = uint8(signedCharClamp(int(qs0)-int(filter1))) ^ 0x80
			plane[s-1] = uint8(signedCharClamp(int(ps0)+int(filter2))) ^ 0x80

			outer := (filter1 + 1) >> 1
			outer &= ^hev

			plane[s+1] = uint8(signedCharClamp(int(qs1)-int(outer))) ^ 0x80
			plane[s-2] = uint8(signedCharClamp(int(ps1)+int(outer))) ^ 0x80
		}
		s += pitch
	}
}

// VpxLpfHorizontal8Dual is vpx_lpf_horizontal_8_dual_c.
func VpxLpfHorizontal8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfHorizontal8Dual(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal8DualScalar(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfHorizontal8Scalar(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfHorizontal8Scalar(plane, s+8, pitch, blimit1, limit1, thresh1)
}

// VpxLpfVertical8Dual is vpx_lpf_vertical_8_dual_c.
func VpxLpfVertical8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfVertical8Dual(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical8DualScalar(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	vpxLpfVertical8Scalar(plane, s, pitch, blimit0, limit0, thresh0)
	vpxLpfVertical8Scalar(plane, s+8*pitch, pitch, blimit1, limit1, thresh1)
}

// mbLpfHorizontalEdgeW mirrors mb_lpf_horizontal_edge_w. count is 1
// for the 16-pixel filter and 2 for the dual variant.
func mbLpfHorizontalEdgeW(plane []uint8, s, pitch int, blimit, limit, thresh uint8, count int) {
	for i := 0; i < 8*count; i++ {
		p3 := plane[s-4*pitch]
		p2 := plane[s-3*pitch]
		p1 := plane[s-2*pitch]
		p0 := plane[s-pitch]
		q0 := plane[s+0]
		q1 := plane[s+1*pitch]
		q2 := plane[s+2*pitch]
		q3 := plane[s+3*pitch]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		if mask != 0 {
			flat := flatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3)
			flat2 := flatMask5(1,
				plane[s-8*pitch], plane[s-7*pitch], plane[s-6*pitch], plane[s-5*pitch],
				p0, q0,
				plane[s+4*pitch], plane[s+5*pitch], plane[s+6*pitch], plane[s+7*pitch])
			filter16(mask, thresh, flat, flat2, plane,
				s-8*pitch, s-7*pitch, s-6*pitch, s-5*pitch,
				s-4*pitch, s-3*pitch, s-2*pitch, s-pitch,
				s, s+pitch, s+2*pitch, s+3*pitch,
				s+4*pitch, s+5*pitch, s+6*pitch, s+7*pitch)
		}
		s++
	}
}

// mbLpfVerticalEdgeW mirrors mb_lpf_vertical_edge_w. count is 8 for
// the 16-pixel filter (covering 8 rows) and 16 for the dual variant.
func mbLpfVerticalEdgeW(plane []uint8, s, pitch int, blimit, limit, thresh uint8, count int) {
	for range count {
		p3 := plane[s-4]
		p2 := plane[s-3]
		p1 := plane[s-2]
		p0 := plane[s-1]
		q0 := plane[s+0]
		q1 := plane[s+1]
		q2 := plane[s+2]
		q3 := plane[s+3]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		if mask != 0 {
			flat := flatMask4(1, p3, p2, p1, p0, q0, q1, q2, q3)
			flat2 := flatMask5(1,
				plane[s-8], plane[s-7], plane[s-6], plane[s-5],
				p0, q0,
				plane[s+4], plane[s+5], plane[s+6], plane[s+7])
			filter16(mask, thresh, flat, flat2, plane,
				s-8, s-7, s-6, s-5, s-4, s-3, s-2, s-1,
				s, s+1, s+2, s+3, s+4, s+5, s+6, s+7)
		}
		s += pitch
	}
}

// VpxLpfHorizontal16 mirrors vpx_lpf_horizontal_16_c.
func VpxLpfHorizontal16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 1)
}

// VpxLpfHorizontal16Dual mirrors vpx_lpf_horizontal_16_dual_c.
func VpxLpfHorizontal16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 2)
}

// VpxLpfVertical16 mirrors vpx_lpf_vertical_16_c.
func VpxLpfVertical16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 8)
}

// VpxLpfVertical16Dual mirrors vpx_lpf_vertical_16_dual_c.
func VpxLpfVertical16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 16)
}

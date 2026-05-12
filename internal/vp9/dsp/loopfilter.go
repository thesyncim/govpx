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
	if a >= b {
		return int(a) - int(b)
	}
	return int(b) - int(a)
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

// VpxLpfHorizontal4 mirrors vpx_lpf_horizontal_4_c. It filters 8
// horizontally-adjacent edge columns above and below the cursor pixel
// `s` (interpreted as offset within `plane`), each spanning 4 rows
// above and 4 below.
func VpxLpfHorizontal4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for i := 0; i < 8; i++ {
		p3 := plane[s-4*pitch]
		p2 := plane[s-3*pitch]
		p1 := plane[s-2*pitch]
		p0 := plane[s-pitch]
		q0 := plane[s+0]
		q1 := plane[s+1*pitch]
		q2 := plane[s+2*pitch]
		q3 := plane[s+3*pitch]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		filter4(mask, thresh, plane, s-2*pitch, s-pitch, s, s+pitch)
		s++
	}
}

// VpxLpfVertical4 mirrors vpx_lpf_vertical_4_c. It filters 8
// vertically-adjacent edge rows left and right of the cursor pixel `s`,
// each spanning 4 columns left and 4 right.
func VpxLpfVertical4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	for i := 0; i < 8; i++ {
		p3 := plane[s-4]
		p2 := plane[s-3]
		p1 := plane[s-2]
		p0 := plane[s-1]
		q0 := plane[s+0]
		q1 := plane[s+1]
		q2 := plane[s+2]
		q3 := plane[s+3]
		mask := filterMask(limit, blimit, p3, p2, p1, p0, q0, q1, q2, q3)
		filter4(mask, thresh, plane, s-2, s-1, s, s+1)
		s += pitch
	}
}

// VpxLpfHorizontal4Dual is vpx_lpf_horizontal_4_dual_c — filter the 8
// columns at `s` with the first threshold triplet, then the next 8 at
// `s+8` with the second.
func VpxLpfHorizontal4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	VpxLpfHorizontal4(plane, s, pitch, blimit0, limit0, thresh0)
	VpxLpfHorizontal4(plane, s+8, pitch, blimit1, limit1, thresh1)
}

// VpxLpfVertical4Dual is vpx_lpf_vertical_4_dual_c — filter the 8 rows
// at `s` with one threshold triplet, then the 8 below at `s+8*pitch`
// with the second.
func VpxLpfVertical4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	VpxLpfVertical4(plane, s, pitch, blimit0, limit0, thresh0)
	VpxLpfVertical4(plane, s+8*pitch, pitch, blimit1, limit1, thresh1)
}

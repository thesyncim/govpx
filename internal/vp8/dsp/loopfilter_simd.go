package dsp

// Vector-friendly mask helpers for the VP8 loop filters (libvpx v1.16.0
// baseline). The dispatch paths use platform kernels or scalar references;
// these helpers remain as compact testable equivalents of the per-lane libvpx
// mask predicates.

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

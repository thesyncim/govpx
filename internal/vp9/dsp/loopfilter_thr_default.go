//go:build !arm64 || purego

package dsp

// Pointer-threshold loopfilter entry points for targets without the
// NEON dispatch: unpack the triplet and defer to the scalar paths.

// VpxLpfHorizontal4Thr is VpxLpfHorizontal4 taking a per-level
// threshold-triplet pointer.
func VpxLpfHorizontal4Thr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfHorizontal4(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

// VpxLpfVertical4Thr is VpxLpfVertical4 taking a threshold pointer.
func VpxLpfVertical4Thr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfVertical4(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

// VpxLpfHorizontal4DualThr is VpxLpfHorizontal4Dual taking threshold
// pointers.
func VpxLpfHorizontal4DualThr(plane []uint8, s, pitch int, t0, t1 *LfThresh) {
	vpxLpfHorizontal4Dual(plane, s, pitch,
		t0.Mblim, t0.Lim, t0.HevThr, t1.Mblim, t1.Lim, t1.HevThr)
}

// VpxLpfVertical4DualThr is VpxLpfVertical4Dual taking threshold
// pointers.
func VpxLpfVertical4DualThr(plane []uint8, s, pitch int, t0, t1 *LfThresh) {
	vpxLpfVertical4Dual(plane, s, pitch,
		t0.Mblim, t0.Lim, t0.HevThr, t1.Mblim, t1.Lim, t1.HevThr)
}

// VpxLpfHorizontal8Thr is VpxLpfHorizontal8 taking a threshold pointer.
func VpxLpfHorizontal8Thr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfHorizontal8(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

// VpxLpfVertical8Thr is VpxLpfVertical8 taking a threshold pointer.
func VpxLpfVertical8Thr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfVertical8(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

// VpxLpfHorizontal8DualThr is VpxLpfHorizontal8Dual taking threshold
// pointers.
func VpxLpfHorizontal8DualThr(plane []uint8, s, pitch int, t0, t1 *LfThresh) {
	vpxLpfHorizontal8Dual(plane, s, pitch,
		t0.Mblim, t0.Lim, t0.HevThr, t1.Mblim, t1.Lim, t1.HevThr)
}

// VpxLpfVertical8DualThr is VpxLpfVertical8Dual taking threshold
// pointers.
func VpxLpfVertical8DualThr(plane []uint8, s, pitch int, t0, t1 *LfThresh) {
	vpxLpfVertical8Dual(plane, s, pitch,
		t0.Mblim, t0.Lim, t0.HevThr, t1.Mblim, t1.Lim, t1.HevThr)
}

// VpxLpfHorizontal16Thr is VpxLpfHorizontal16 taking a threshold
// pointer.
func VpxLpfHorizontal16Thr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfHorizontal16(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

// VpxLpfHorizontal16DualThr is VpxLpfHorizontal16Dual taking a
// threshold pointer.
func VpxLpfHorizontal16DualThr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfHorizontal16Dual(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

// VpxLpfVertical16Thr is VpxLpfVertical16 taking a threshold pointer.
func VpxLpfVertical16Thr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfVertical16(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

// VpxLpfVertical16DualThr is VpxLpfVertical16Dual taking a threshold
// pointer.
func VpxLpfVertical16DualThr(plane []uint8, s, pitch int, t *LfThresh) {
	vpxLpfVertical16Dual(plane, s, pitch, t.Mblim, t.Lim, t.HevThr)
}

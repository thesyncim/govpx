package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
)

func vpxLpfHorizontal4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit), int(limit), int(thresh), 4) {
		return
	}

	vpxLpfHorizontal4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical4(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit), int(limit), int(thresh), 4) {
		return
	}

	vpxLpfVertical4Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit0), int(limit0), int(thresh0), 4) {
		pixel.LoopFilter(plane, s+8*1, pitch, 1, 8, int(blimit1), int(limit1), int(thresh1), 4)
		return
	}

	vpxLpfHorizontal4DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical4Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit0), int(limit0), int(thresh0), 4) {
		pixel.LoopFilter(plane, s+8*pitch, 1, pitch, 8, int(blimit1), int(limit1), int(thresh1), 4)
		return
	}

	vpxLpfVertical4DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit), int(limit), int(thresh), 8) {
		return
	}

	vpxLpfHorizontal8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfVertical8(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit), int(limit), int(thresh), 8) {
		return
	}

	vpxLpfVertical8Scalar(plane, s, pitch, blimit, limit, thresh)
}

func vpxLpfHorizontal8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if blimit0 == blimit1 && limit0 == limit1 && thresh0 == thresh1 &&
		pixel.LoopFilter(plane, s, pitch, 1, 16, int(blimit0), int(limit0), int(thresh0), 8) {
		return
	}
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit0), int(limit0), int(thresh0), 8) {
		pixel.LoopFilter(plane, s+8*1, pitch, 1, 8, int(blimit1), int(limit1), int(thresh1), 8)
		return
	}

	vpxLpfHorizontal8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfVertical8Dual(plane []uint8, s, pitch int,
	blimit0, limit0, thresh0, blimit1, limit1, thresh1 uint8,
) {
	if blimit0 == blimit1 && limit0 == limit1 && thresh0 == thresh1 &&
		pixel.LoopFilter(plane, s, 1, pitch, 16, int(blimit0), int(limit0), int(thresh0), 8) {
		return
	}
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit0), int(limit0), int(thresh0), 8) {
		pixel.LoopFilter(plane, s+8*pitch, 1, pitch, 8, int(blimit1), int(limit1), int(thresh1), 8)
		return
	}

	vpxLpfVertical8DualScalar(plane, s, pitch, blimit0, limit0, thresh0, blimit1, limit1, thresh1)
}

func vpxLpfHorizontal16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 8, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 1)
}

func vpxLpfHorizontal16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, pitch, 1, 16, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfHorizontalEdgeW(plane, s, pitch, blimit, limit, thresh, 2)
}

func vpxLpfVertical16(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 8, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 8)
}

func vpxLpfVertical16Dual(plane []uint8, s, pitch int, blimit, limit, thresh uint8) {
	if pixel.LoopFilter(plane, s, 1, pitch, 16, int(blimit), int(limit), int(thresh), 16) {
		return
	}

	mbLpfVerticalEdgeW(plane, s, pitch, blimit, limit, thresh, 16)
}

// LfThresh mirrors libvpx's loop_filter_thresh triplet: the macroblock
// edge limit, the interior limit, and the high-edge-variance threshold
// for one filter level. libvpx stores each value pre-broadcast to a
// SIMD_WIDTH vector; the NEON kernels here dup-load single bytes, so
// the scalar triplet is enough. The three fields are guaranteed to be
// laid out at byte offsets 0/1/2 (asserted in the arm64 dispatch),
// letting kernels take one pointer per filter level exactly like
// libvpx's lfthr pointer-based dispatch.
type LfThresh struct {
	Mblim  uint8
	Lim    uint8
	HevThr uint8
}

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

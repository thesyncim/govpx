package dsp

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

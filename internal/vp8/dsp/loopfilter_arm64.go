//go:build arm64

package dsp

// NEON kernels for the VP8 loop-filter apply paths. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/{vp8_loopfilter,mbloopfilter}_neon.c
// 16-wide horizontal-edge variants vp8_loop_filter_neon and
// vp8_mbloop_filter_neon respectively. These take a pointer
// to the start of the p3 row of an 8-row by 16-column horizontal-edge
// window (or, for vertical-edge use, a transposed buffer), the pitch
// (row stride), and the three filter parameters. They write back the
// 4 (loopFilter) or 6 (mbLoopFilter) modified rows.

//go:noescape
func loopFilterEdgeH16NEON(src *byte, pitch int, blimit, limit, thresh byte)

//go:noescape
func mbLoopFilterEdgeH16NEON(src *byte, pitch int, blimit, limit, thresh byte)

// Direct vertical-edge variants (libvpx vp8_loop_filter_vertical_edge_y_neon
// and vp8_mbloop_filter_vertical_edge_y_neon). These take a pointer at the
// q0 column of row 0 (filter is applied around the edge between bytes -1
// and 0); they read 8 bytes per row at src-4 across 16 rows, transpose,
// filter in-register, transpose back, and write modified bytes back.

//go:noescape
func loopFilterEdgeV16NEON(src *byte, pitch int, blimit, limit, thresh byte)

//go:noescape
func mbLoopFilterEdgeV16NEON(src *byte, pitch int, blimit, limit, thresh byte)

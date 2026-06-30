//go:build arm64 && !purego

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

// LoopFilterEdgeH16NEON exposes the byte-identical 4-tap loopfilter kernel
// to sibling codec packages that share libvpx's vpx_dsp narrow filter.
func LoopFilterEdgeH16NEON(src *byte, pitch int, blimit, limit, thresh byte) {
	loopFilterEdgeH16NEON(src, pitch, blimit, limit, thresh)
}

// Direct vertical-edge variants (libvpx vp8_loop_filter_vertical_edge_y_neon
// and vp8_mbloop_filter_vertical_edge_y_neon). These take a pointer at the
// q0 column of row 0 (filter is applied around the edge between bytes -1
// and 0); they read 8 bytes per row at src-4 across 16 rows, transpose,
// filter in-register, transpose back, and write modified bytes back.

//go:noescape
func loopFilterEdgeV16NEON(src *byte, pitch int, blimit, limit, thresh byte)

//go:noescape
func mbLoopFilterEdgeV16NEON(src *byte, pitch int, blimit, limit, thresh byte)

// LoopFilterEdgeV16NEON exposes the byte-identical 4-tap vertical loopfilter
// kernel to sibling codec packages.
func LoopFilterEdgeV16NEON(src *byte, pitch int, blimit, limit, thresh byte) {
	loopFilterEdgeV16NEON(src, pitch, blimit, limit, thresh)
}

// Direct vertical-edge UV pair variant (libvpx
// vp8_loop_filter_vertical_edge_uv_neon). The u/v pointers are at the q0
// column of row 0; the kernel reads 8 bytes per row at pointer-4 across
// 8 rows for each plane and writes p1,p0,q0,q1 back.
//
//go:noescape
func loopFilterEdgeV8x8PairNEON(u *byte, v *byte, pitch int, blimit, limit, thresh byte)

// Direct vertical-edge UV pair variant (libvpx
// vp8_mbloop_filter_vertical_edge_uv_neon). The u/v pointers are at the q0
// column of row 0; the kernel reads 8 bytes per row at pointer-4 across
// 8 rows for each plane and writes p2,p1,p0,q0,q1,q2 back.
//
//go:noescape
func mbLoopFilterEdgeV8x8PairNEON(u *byte, v *byte, pitch int, blimit, limit, thresh byte)

// NEON kernel for the VP8 simple loop filter, horizontal edge variant
// (libvpx vp8_loop_filter_simple_horizontal_edge_neon). Caller passes a
// pointer at the p1 row; the kernel reads p1, p0, q0, q1 at +pitch
// increments and writes p0 and q0 back.
//
//go:noescape
func loopFilterSimpleEdgeH16NEON(src *byte, pitch int, blimit byte)

// NEON kernel for the VP8 simple loop filter, vertical edge variant
// (libvpx vp8_loop_filter_simple_vertical_edge_neon). Caller passes a
// pointer at the q0 column of row 0 (filter is applied around the edge
// between bytes -1 and 0). The kernel reads 4 bytes per row at src-2
// across 16 rows, transposes, filters, and writes 2 modified bytes
// (p0 and q0) at offset src-1 across 16 rows.
//
//go:noescape
func loopFilterSimpleEdgeV16NEON(src *byte, pitch int, blimit byte)

//go:build amd64 && !purego

package dsp

// SSE2 kernels for the VP8 loop-filter apply paths (libvpx v1.16.0
// baseline). Mirror vp8/common/x86/loopfilter_sse2.asm (LFH_FILTER_AND_HEV_MASK +
// B_FILTER for inner LF; MB_FILTER_AND_WRITEBACK for MB LF) but operate
// on a horizontal 16-wide window of 8 contiguous rows (pointer at the
// p3 row, pitch = pitch). Vertical-edge variants gather the 16x8
// row-window into a transposed 8x16 stack buffer and reuse the same
// kernels — same pattern as the NEON arm64 port.

//go:noescape
func loopFilterEdgeH16SSE2(src *byte, pitch int, blimit, limit, thresh byte)

//go:noescape
func mbLoopFilterEdgeH16SSE2(src *byte, pitch int, blimit, limit, thresh byte)

// AVX2 (VEX-encoded) variants of the inner / mb LF horizontal-edge
// kernels. Same 16-wide window as the SSE2 paths but use 3-operand
// non-destructive instructions to eliminate the per-step MOVOU copies.
// Gated at runtime by internal/cpu.HasAVX2.

//go:noescape
func loopFilterEdgeH16AVX2(src *byte, pitch int, blimit, limit, thresh byte)

//go:noescape
func mbLoopFilterEdgeH16AVX2(src *byte, pitch int, blimit, limit, thresh byte)

// SSE2 16x8 byte transpose helpers used by luma vertical-edge filters.
// They replace the Go gather/scatter byte loops around the shared
// horizontal LF kernels.
//
//go:noescape
func gatherV16x8AMD64SSE2(tmp *[8 * 16]byte, src *byte, stride int)

//go:noescape
func scatterV16x8AMD64SSE2(dst *byte, stride int, tmp *[8 * 16]byte)

// loopFilterSimpleEdgeH16SSE2 mirrors libvpx
// vp8_loop_filter_simple_horizontal_edge_sse2 (vp8/common/x86/loopfilter_sse2.asm).
// Caller passes a pointer at the p1 row of an 8-row by 16-column window;
// the kernel reads p1=row0, p0=row1, q0=row2, q1=row3 at +pitch
// increments and writes p0,q0 back. The vertical-edge variant gathers
// 16x4 columns into a transposed 4x16 stack buffer, runs the same
// kernel, then scatters the 2 modified rows back.
//
//go:noescape
func loopFilterSimpleEdgeH16SSE2(src *byte, pitch int, blimit byte)

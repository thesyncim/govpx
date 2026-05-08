//go:build amd64

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

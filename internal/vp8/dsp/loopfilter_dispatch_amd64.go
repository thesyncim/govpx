//go:build amd64

package dsp

// amd64 dispatch for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).
// Currently routes through the libvpx-style scalar reference; SSE2
// kernels mirroring vp8/common/x86/loopfilter_sse2.asm are a follow-up
// in this round series — the dispatch wrapper is in place so the SSE2
// drop-in only needs to land the assembly without rewiring callers.

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

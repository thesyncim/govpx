//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).

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

func loopFilterSimpleHorizontalEdgeDispatch(s []byte, stride int, blimit byte) {
	loopFilterSimpleHorizontalEdgeScalar(s, stride, blimit)
}

func loopFilterSimpleVerticalEdgeDispatch(s []byte, stride int, blimit byte) {
	loopFilterSimpleVerticalEdgeScalar(s, stride, blimit)
}

//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterHorizontalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	loopFilterHorizontalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[4*stride:], stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[8*stride:], stride, blimit, limit, thresh, 2)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	loopFilterVerticalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[4:], stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[8:], stride, blimit, limit, thresh, 2)
}

func loopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	loopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func loopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	loopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	mbLoopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	mbLoopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func loopFilterSimpleHorizontalEdgeDispatch(s []byte, stride int, blimit byte) {
	loopFilterSimpleHorizontalEdgeScalar(s, stride, blimit)
}

func loopFilterSimpleVerticalEdgeDispatch(s []byte, stride int, blimit byte) {
	loopFilterSimpleVerticalEdgeScalar(s, stride, blimit)
}

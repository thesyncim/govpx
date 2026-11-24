package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Pure-Go fallback for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if pixel.LoopFilter(s, 4*stride, stride, 1, count*8, int(blimit), int(limit), int(thresh), 4) {
		return
	}

	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterHorizontalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	if pixel.LoopFilterInternal16(s, stride, 1, int(blimit), int(limit), int(thresh)) {
		return
	}
	loopFilterHorizontalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[4*stride:], stride, blimit, limit, thresh, 2)
	loopFilterHorizontalEdgeDispatch(s[8*stride:], stride, blimit, limit, thresh, 2)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if pixel.LoopFilter(s, 4*1, 1, stride, count*8, int(blimit), int(limit), int(thresh), 4) {
		return
	}

	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgesYDispatch(s []byte, stride int, blimit, limit, thresh byte) {
	if pixel.LoopFilterInternal16(s, 1, stride, int(blimit), int(limit), int(thresh)) {
		return
	}
	loopFilterVerticalEdgeDispatch(s, stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[4:], stride, blimit, limit, thresh, 2)
	loopFilterVerticalEdgeDispatch(s[8:], stride, blimit, limit, thresh, 2)
}

func loopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if pixel.LoopFilterUV8(u, v, stride, 1, int(blimit), int(limit), int(thresh), 4) {
		return
	}
	loopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func loopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if pixel.LoopFilterUV8(u, v, 1, stride, int(blimit), int(limit), int(thresh), 4) {
		return
	}
	loopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	loopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if pixel.LoopFilter(s, 4*stride, stride, 1, count*8, int(blimit), int(limit), int(thresh), 6) {
		return
	}

	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if pixel.LoopFilter(s, 4*1, 1, stride, count*8, int(blimit), int(limit), int(thresh), 6) {
		return
	}

	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if pixel.LoopFilterUV8(u, v, stride, 1, int(blimit), int(limit), int(thresh), 6) {
		return
	}
	mbLoopFilterHorizontalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterHorizontalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func mbLoopFilterVerticalEdgeUVDispatch(u []byte, v []byte, stride int, blimit, limit, thresh byte) {
	if pixel.LoopFilterUV8(u, v, 1, stride, int(blimit), int(limit), int(thresh), 6) {
		return
	}
	mbLoopFilterVerticalEdgeDispatch(u, stride, blimit, limit, thresh, 1)
	mbLoopFilterVerticalEdgeDispatch(v, stride, blimit, limit, thresh, 1)
}

func loopFilterSimpleHorizontalEdgeDispatch(s []byte, stride int, blimit byte) {
	if pixel.LoopFilterSimpleHorizontal16(s, stride, blimit) {
		return
	}
	if pixel.LoopFilter(s, 2*stride, stride, 1, 16, int(blimit), 0, 0, 2) {
		return
	}

	loopFilterSimpleHorizontalEdgeScalar(s, stride, blimit)
}

func loopFilterSimpleVerticalEdgeDispatch(s []byte, stride int, blimit byte) {
	if pixel.LoopFilterSimpleVertical16(s, stride, blimit) {
		return
	}
	if pixel.LoopFilter(s, 2*1, 1, stride, 16, int(blimit), 0, 0, 2) {
		return
	}

	loopFilterSimpleVerticalEdgeScalar(s, stride, blimit)
}

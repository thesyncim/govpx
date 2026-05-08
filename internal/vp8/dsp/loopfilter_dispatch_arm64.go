//go:build arm64

package dsp

// arm64 dispatch for VP8 loop-filter apply paths (libvpx v1.16.0 baseline).
// NEON kernels handle the 16-wide (count=2) horizontal-edge cases for
// both inner and MB filters as well as the vertical edges via direct
// register-level transpose paths (TRN1/TRN2 cascade on .4S/.8H/.16B).
// count=1 (chroma 8-wide) uses the libvpx-style scalar reference.

func loopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		loopFilterEdgeH16NEON(&s[0], stride, blimit, limit, thresh)
		return
	}
	loopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func loopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		// Kernel follows libvpx: caller passes a pointer at the q0
		// column (the byte at offset +4 from the leftmost p3); the
		// kernel reads 8 bytes/row at src-4 across 16 rows.
		loopFilterEdgeV16NEON(&s[4], stride, blimit, limit, thresh)
		return
	}
	loopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterHorizontalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 7*stride+16 {
		mbLoopFilterEdgeH16NEON(&s[0], stride, blimit, limit, thresh)
		return
	}
	mbLoopFilterHorizontalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

func mbLoopFilterVerticalEdgeDispatch(s []byte, stride int, blimit, limit, thresh byte, count int) {
	if count == 2 && len(s) >= 15*stride+8 {
		mbLoopFilterEdgeV16NEON(&s[4], stride, blimit, limit, thresh)
		return
	}
	mbLoopFilterVerticalEdgeScalar(s, stride, blimit, limit, thresh, count)
}

// loopFilterSimpleHorizontalEdgeDispatch routes the 16-wide simple-LF
// horizontal edge through the NEON kernel when the input window is
// large enough; otherwise falls back to the libvpx-style scalar.
func loopFilterSimpleHorizontalEdgeDispatch(s []byte, stride int, blimit byte) {
	if len(s) >= 3*stride+16 {
		loopFilterSimpleEdgeH16NEON(&s[0], stride, blimit)
		return
	}
	loopFilterSimpleHorizontalEdgeScalar(s, stride, blimit)
}

// loopFilterSimpleVerticalEdgeDispatch invokes the direct vertical-edge
// NEON kernel: caller passes the slice base; the kernel reads 4 bytes
// per row at &s[2]-2 = &s[0] across 16 rows and writes 2 modified bytes
// per row at &s[2]-1 = &s[1].
func loopFilterSimpleVerticalEdgeDispatch(s []byte, stride int, blimit byte) {
	if len(s) >= 15*stride+4 {
		loopFilterSimpleEdgeV16NEON(&s[2], stride, blimit)
		return
	}
	loopFilterSimpleVerticalEdgeScalar(s, stride, blimit)
}

package dsp

import (
	"testing"
)

// FuzzVP8DSPLoopfilter is a differential SIMD-vs-scalar fuzz harness
// for the VP8 loop filter family. Mirrors libvpx
// test/vp8_loopfilter_test.cc cross-check pattern.
//
// Op selector covers:
//
//	0  LoopFilterHorizontalEdge / scalar
//	1  LoopFilterVerticalEdge / scalar
//	2  MBLoopFilterHorizontalEdge / scalar
//	3  MBLoopFilterVerticalEdge / scalar
//	4  LoopFilterSimpleHorizontalEdge / scalar
//	5  LoopFilterSimpleVerticalEdge / scalar
//
// The simple horizontal/vertical SIMD kernel uses uqadd saturation on
// the (2*|p0-q0|, |p1-q1|/2) composite — libvpx-inherited behaviour.
// The fuzz callback clamps blimit to 200 to stay inside the realistic
// per-frame range and avoid the documented saturation-vs-int divergence
// at blimit==255 (see loopfilter_simd_test.go:266-275).

func FuzzVP8DSPLoopfilter(f *testing.F) {
	seeds := [][]byte{
		make([]byte, 1024),
		bytes255(1024),
		bytesAlt(1024),
		bytesRamp(1024, 0),
		bytesRamp(1024, 11),
		bytesPattern(1024, 0x7C, 0x4B),
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, data []byte) {
		const stride = 32
		const height = 16
		const planeBytes = stride * height
		if len(data) < 4+planeBytes {
			return
		}
		op := int(data[0]) % 6
		blimit := data[1]
		limit := data[2] & 63
		thresh := data[3] & 31
		if blimit > 200 {
			// Avoid the documented saturating-vs-int divergence at
			// blimit==255; libvpx never feeds the simple filter such a
			// value in practice (2*filter_level + sharpness <= 200).
			blimit = 200
		}

		base := make([]byte, planeBytes)
		copy(base, data[4:4+planeBytes])

		gotBuf := append([]byte(nil), base...)
		wantBuf := append([]byte(nil), base...)

		switch op {
		case 0:
			loopFilterHorizontalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			loopFilterHorizontalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 1:
			loopFilterVerticalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			loopFilterVerticalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 2:
			mbLoopFilterHorizontalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			mbLoopFilterHorizontalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 3:
			mbLoopFilterVerticalEdgeDispatch(gotBuf, stride, blimit, limit, thresh, 2)
			mbLoopFilterVerticalEdgeScalar(wantBuf, stride, blimit, limit, thresh, 2)
		case 4:
			loopFilterSimpleHorizontalEdgeDispatch(gotBuf, stride, blimit)
			loopFilterSimpleHorizontalEdgeScalar(wantBuf, stride, blimit)
		case 5:
			loopFilterSimpleVerticalEdgeDispatch(gotBuf, stride, blimit)
			loopFilterSimpleVerticalEdgeScalar(wantBuf, stride, blimit)
		}

		for i, w := range wantBuf {
			if g := gotBuf[i]; g != w {
				t.Fatalf("op=%d blimit=%d limit=%d thresh=%d byte %d: simd=%d scalar=%d",
					op, blimit, limit, thresh, i, g, w)
			}
		}
	})
}

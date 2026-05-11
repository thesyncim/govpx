package dsp

import "github.com/thesyncim/govpx/internal/vp8/common"

// Ported from libvpx v1.16.0 vpx_dsp/intrapred.c and
// vp8/common/reconintra4x4.c.

func Intra4x4Predict(dst []byte, dstStride int, mode common.BPredictionMode, above []byte, left []byte, topLeft byte) bool {
	switch mode {
	case common.BDCPred:
		Intra4x4DCPredict(dst, dstStride, above, left)
	case common.BTMPred:
		Intra4x4TMPredict(dst, dstStride, above, left, topLeft)
	case common.BVEPred:
		Intra4x4VEPredict(dst, dstStride, above, topLeft)
	case common.BHEPred:
		Intra4x4HEPredict(dst, dstStride, left, topLeft)
	case common.BLDPred:
		Intra4x4LDPredict(dst, dstStride, above)
	case common.BRDPred:
		Intra4x4RDPredict(dst, dstStride, above, left, topLeft)
	case common.BVRPred:
		Intra4x4VRPredict(dst, dstStride, above, left, topLeft)
	case common.BVLPred:
		Intra4x4VLPredict(dst, dstStride, above)
	case common.BHDPred:
		Intra4x4HDPredict(dst, dstStride, above, left, topLeft)
	case common.BHUPred:
		Intra4x4HUPredict(dst, dstStride, left)
	default:
		return false
	}
	return true
}

func Intra4x4DCPredict(dst []byte, dstStride int, above []byte, left []byte) {
	_ = above[3]
	_ = left[3]
	_ = dst[3*dstStride+3]
	intra4x4DCPredict(dst, dstStride, above, left)
}

func Intra4x4TMPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	_ = above[3]
	_ = left[3]
	_ = dst[3*dstStride+3]
	intra4x4TMPredict(dst, dstStride, above, left, topLeft)
}

func Intra4x4VEPredict(dst []byte, dstStride int, above []byte, topLeft byte) {
	_ = above[4]
	_ = dst[3*dstStride+3]
	intra4x4VEPredict(dst, dstStride, above, topLeft)
}

func Intra4x4HEPredict(dst []byte, dstStride int, left []byte, topLeft byte) {
	_ = left[3]
	_ = dst[3*dstStride+3]
	intra4x4HEPredict(dst, dstStride, left, topLeft)
}

func Intra4x4LDPredict(dst []byte, dstStride int, above []byte) {
	_ = above[7]
	_ = dst[3*dstStride+3]
	intra4x4LDPredict(dst, dstStride, above)
}

func Intra4x4RDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	_ = above[3]
	_ = left[3]
	_ = dst[3*dstStride+3]
	intra4x4RDPredict(dst, dstStride, above, left, topLeft)
}

func Intra4x4VRPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	_ = above[3]
	_ = left[2]
	_ = dst[3*dstStride+3]
	intra4x4VRPredict(dst, dstStride, above, left, topLeft)
}

func Intra4x4VLPredict(dst []byte, dstStride int, above []byte) {
	_ = above[7]
	_ = dst[3*dstStride+3]
	intra4x4VLPredict(dst, dstStride, above)
}

func Intra4x4HDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	_ = above[2]
	_ = left[3]
	_ = dst[3*dstStride+3]
	intra4x4HDPredict(dst, dstStride, above, left, topLeft)
}

func Intra4x4HUPredict(dst []byte, dstStride int, left []byte) {
	_ = left[3]
	_ = dst[3*dstStride+3]
	intra4x4HUPredict(dst, dstStride, left)
}

// Scalar reference kernels for the 4x4 B_PRED modes. These are the
// authoritative bit-exact implementations; SIMD dispatch files on amd64
// and arm64 may shadow these names but must produce byte-identical
// output. Mirrors libvpx v1.16.0 vp8/common/reconintra4x4.c.

func intra4x4DCPredictScalar(dst []byte, dstStride int, above []byte, left []byte) {
	intraDCPredictScalar(dst, dstStride, above, left, 4, true, true)
}

func intra4x4TMPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intraTMPredictScalar(dst, dstStride, above, left, topLeft, 4)
}

func intra4x4VEPredictScalar(dst []byte, dstStride int, above []byte, topLeft byte) {
	row := [4]byte{
		avg3(topLeft, above[0], above[1]),
		avg3(above[0], above[1], above[2]),
		avg3(above[1], above[2], above[3]),
		avg3(above[2], above[3], above[4]),
	}
	for y := range 4 {
		copy(dst[y*dstStride:y*dstStride+4], row[:])
	}
}

func intra4x4HEPredictScalar(dst []byte, dstStride int, left []byte, topLeft byte) {
	fillRow4(dst[0*dstStride:], avg3(topLeft, left[0], left[1]))
	fillRow4(dst[1*dstStride:], avg3(left[0], left[1], left[2]))
	fillRow4(dst[2*dstStride:], avg3(left[1], left[2], left[3]))
	fillRow4(dst[3*dstStride:], avg3(left[2], left[3], left[3]))
}

func intra4x4LDPredictScalar(dst []byte, dstStride int, above []byte) {
	// Pin each output row to a 4-byte array view so the per-cell writes
	// run without bounds checks. The eight above[] taps go through a
	// single pointer view for the same reason.
	a8 := (*[8]byte)(above[:8])
	r0 := (*[4]byte)(dst[0*dstStride : 0*dstStride+4])
	r1 := (*[4]byte)(dst[1*dstStride : 1*dstStride+4])
	r2 := (*[4]byte)(dst[2*dstStride : 2*dstStride+4])
	r3 := (*[4]byte)(dst[3*dstStride : 3*dstStride+4])
	a, b, c, d := a8[0], a8[1], a8[2], a8[3]
	e, f, g, h := a8[4], a8[5], a8[6], a8[7]

	r0[0] = avg3(a, b, c)
	r0[1] = avg3(b, c, d)
	r1[0] = r0[1]
	r0[2] = avg3(c, d, e)
	r1[1] = r0[2]
	r2[0] = r0[2]
	r0[3] = avg3(d, e, f)
	r1[2] = r0[3]
	r2[1] = r0[3]
	r3[0] = r0[3]
	r1[3] = avg3(e, f, g)
	r2[2] = r1[3]
	r3[1] = r1[3]
	r2[3] = avg3(f, g, h)
	r3[2] = r2[3]
	r3[3] = avg3(g, h, h)
}

func intra4x4RDPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	a4 := (*[4]byte)(above[:4])
	l4 := (*[4]byte)(left[:4])
	r0 := (*[4]byte)(dst[0*dstStride : 0*dstStride+4])
	r1 := (*[4]byte)(dst[1*dstStride : 1*dstStride+4])
	r2 := (*[4]byte)(dst[2*dstStride : 2*dstStride+4])
	r3 := (*[4]byte)(dst[3*dstStride : 3*dstStride+4])
	a, b, c, d := a4[0], a4[1], a4[2], a4[3]
	i, j, k, l := l4[0], l4[1], l4[2], l4[3]
	x := topLeft

	r3[0] = avg3(j, k, l)
	r3[1] = avg3(i, j, k)
	r2[0] = r3[1]
	r3[2] = avg3(x, i, j)
	r2[1] = r3[2]
	r1[0] = r3[2]
	r3[3] = avg3(a, x, i)
	r2[2] = r3[3]
	r1[1] = r3[3]
	r0[0] = r3[3]
	r2[3] = avg3(b, a, x)
	r1[2] = r2[3]
	r0[1] = r2[3]
	r1[3] = avg3(c, b, a)
	r0[2] = r1[3]
	r0[3] = avg3(d, c, b)
}

func intra4x4VRPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	a4 := (*[4]byte)(above[:4])
	l3 := (*[3]byte)(left[:3])
	r0 := (*[4]byte)(dst[0*dstStride : 0*dstStride+4])
	r1 := (*[4]byte)(dst[1*dstStride : 1*dstStride+4])
	r2 := (*[4]byte)(dst[2*dstStride : 2*dstStride+4])
	r3 := (*[4]byte)(dst[3*dstStride : 3*dstStride+4])
	a, b, c, d := a4[0], a4[1], a4[2], a4[3]
	i, j, k := l3[0], l3[1], l3[2]
	x := topLeft

	r0[0] = avg2(x, a)
	r2[1] = r0[0]
	r0[1] = avg2(a, b)
	r2[2] = r0[1]
	r0[2] = avg2(b, c)
	r2[3] = r0[2]
	r0[3] = avg2(c, d)

	r3[0] = avg3(k, j, i)
	r2[0] = avg3(j, i, x)
	r1[0] = avg3(i, x, a)
	r3[1] = r1[0]
	r1[1] = avg3(x, a, b)
	r3[2] = r1[1]
	r1[2] = avg3(a, b, c)
	r3[3] = r1[2]
	r1[3] = avg3(b, c, d)
}

func intra4x4VLPredictScalar(dst []byte, dstStride int, above []byte) {
	a8 := (*[8]byte)(above[:8])
	r0 := (*[4]byte)(dst[0*dstStride : 0*dstStride+4])
	r1 := (*[4]byte)(dst[1*dstStride : 1*dstStride+4])
	r2 := (*[4]byte)(dst[2*dstStride : 2*dstStride+4])
	r3 := (*[4]byte)(dst[3*dstStride : 3*dstStride+4])
	a, b, c, d := a8[0], a8[1], a8[2], a8[3]
	e, f, g, h := a8[4], a8[5], a8[6], a8[7]

	r0[0] = avg2(a, b)
	r0[1] = avg2(b, c)
	r2[0] = r0[1]
	r0[2] = avg2(c, d)
	r2[1] = r0[2]
	r0[3] = avg2(d, e)
	r2[2] = r0[3]
	r2[3] = avg3(e, f, g)

	r1[0] = avg3(a, b, c)
	r1[1] = avg3(b, c, d)
	r3[0] = r1[1]
	r1[2] = avg3(c, d, e)
	r3[1] = r1[2]
	r1[3] = avg3(d, e, f)
	r3[2] = r1[3]
	r3[3] = avg3(f, g, h)
}

func intra4x4HDPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	a3 := (*[3]byte)(above[:3])
	l4 := (*[4]byte)(left[:4])
	r0 := (*[4]byte)(dst[0*dstStride : 0*dstStride+4])
	r1 := (*[4]byte)(dst[1*dstStride : 1*dstStride+4])
	r2 := (*[4]byte)(dst[2*dstStride : 2*dstStride+4])
	r3 := (*[4]byte)(dst[3*dstStride : 3*dstStride+4])
	a, b, c := a3[0], a3[1], a3[2]
	i, j, k, l := l4[0], l4[1], l4[2], l4[3]
	x := topLeft

	r0[0] = avg2(i, x)
	r1[2] = r0[0]
	r1[0] = avg2(j, i)
	r2[2] = r1[0]
	r2[0] = avg2(k, j)
	r3[2] = r2[0]
	r3[0] = avg2(l, k)

	r0[3] = avg3(a, b, c)
	r0[2] = avg3(x, a, b)
	r0[1] = avg3(i, x, a)
	r1[3] = r0[1]
	r1[1] = avg3(j, i, x)
	r2[3] = r1[1]
	r2[1] = avg3(k, j, i)
	r3[3] = r2[1]
	r3[1] = avg3(l, k, j)
}

func intra4x4HUPredictScalar(dst []byte, dstStride int, left []byte) {
	l4 := (*[4]byte)(left[:4])
	r0 := (*[4]byte)(dst[0*dstStride : 0*dstStride+4])
	r1 := (*[4]byte)(dst[1*dstStride : 1*dstStride+4])
	r2 := (*[4]byte)(dst[2*dstStride : 2*dstStride+4])
	r3 := (*[4]byte)(dst[3*dstStride : 3*dstStride+4])
	i, j, k, l := l4[0], l4[1], l4[2], l4[3]

	r0[0] = avg2(i, j)
	r0[2] = avg2(j, k)
	r1[0] = r0[2]
	r1[2] = avg2(k, l)
	r2[0] = r1[2]
	r0[1] = avg3(i, j, k)
	r0[3] = avg3(j, k, l)
	r1[1] = r0[3]
	r1[3] = avg3(k, l, l)
	r2[1] = r1[3]
	r2[3] = l
	r2[2] = l
	r3[0] = l
	r3[1] = l
	r3[2] = l
	r3[3] = l
}

func avg2(a byte, b byte) byte {
	return byte((int(a) + int(b) + 1) >> 1)
}

func avg3(a byte, b byte, c byte) byte {
	return byte((int(a) + 2*int(b) + int(c) + 2) >> 2)
}

func fillRow4(dst []byte, v byte) {
	_ = dst[3]
	dst[0] = v
	dst[1] = v
	dst[2] = v
	dst[3] = v
}

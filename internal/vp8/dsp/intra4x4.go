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
	a, b, c, d := above[0], above[1], above[2], above[3]
	e, f, g, h := above[4], above[5], above[6], above[7]

	dst[0*dstStride+0] = avg3(a, b, c)
	dst[0*dstStride+1] = avg3(b, c, d)
	dst[1*dstStride+0] = dst[0*dstStride+1]
	dst[0*dstStride+2] = avg3(c, d, e)
	dst[1*dstStride+1] = dst[0*dstStride+2]
	dst[2*dstStride+0] = dst[0*dstStride+2]
	dst[0*dstStride+3] = avg3(d, e, f)
	dst[1*dstStride+2] = dst[0*dstStride+3]
	dst[2*dstStride+1] = dst[0*dstStride+3]
	dst[3*dstStride+0] = dst[0*dstStride+3]
	dst[1*dstStride+3] = avg3(e, f, g)
	dst[2*dstStride+2] = dst[1*dstStride+3]
	dst[3*dstStride+1] = dst[1*dstStride+3]
	dst[2*dstStride+3] = avg3(f, g, h)
	dst[3*dstStride+2] = dst[2*dstStride+3]
	dst[3*dstStride+3] = avg3(g, h, h)
}

func intra4x4RDPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	a, b, c, d := above[0], above[1], above[2], above[3]
	i, j, k, l := left[0], left[1], left[2], left[3]
	x := topLeft

	dst[3*dstStride+0] = avg3(j, k, l)
	dst[3*dstStride+1] = avg3(i, j, k)
	dst[2*dstStride+0] = dst[3*dstStride+1]
	dst[3*dstStride+2] = avg3(x, i, j)
	dst[2*dstStride+1] = dst[3*dstStride+2]
	dst[1*dstStride+0] = dst[3*dstStride+2]
	dst[3*dstStride+3] = avg3(a, x, i)
	dst[2*dstStride+2] = dst[3*dstStride+3]
	dst[1*dstStride+1] = dst[3*dstStride+3]
	dst[0*dstStride+0] = dst[3*dstStride+3]
	dst[2*dstStride+3] = avg3(b, a, x)
	dst[1*dstStride+2] = dst[2*dstStride+3]
	dst[0*dstStride+1] = dst[2*dstStride+3]
	dst[1*dstStride+3] = avg3(c, b, a)
	dst[0*dstStride+2] = dst[1*dstStride+3]
	dst[0*dstStride+3] = avg3(d, c, b)
}

func intra4x4VRPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	a, b, c, d := above[0], above[1], above[2], above[3]
	i, j, k := left[0], left[1], left[2]
	x := topLeft

	dst[0*dstStride+0] = avg2(x, a)
	dst[2*dstStride+1] = dst[0*dstStride+0]
	dst[0*dstStride+1] = avg2(a, b)
	dst[2*dstStride+2] = dst[0*dstStride+1]
	dst[0*dstStride+2] = avg2(b, c)
	dst[2*dstStride+3] = dst[0*dstStride+2]
	dst[0*dstStride+3] = avg2(c, d)

	dst[3*dstStride+0] = avg3(k, j, i)
	dst[2*dstStride+0] = avg3(j, i, x)
	dst[1*dstStride+0] = avg3(i, x, a)
	dst[3*dstStride+1] = dst[1*dstStride+0]
	dst[1*dstStride+1] = avg3(x, a, b)
	dst[3*dstStride+2] = dst[1*dstStride+1]
	dst[1*dstStride+2] = avg3(a, b, c)
	dst[3*dstStride+3] = dst[1*dstStride+2]
	dst[1*dstStride+3] = avg3(b, c, d)
}

func intra4x4VLPredictScalar(dst []byte, dstStride int, above []byte) {
	a, b, c, d := above[0], above[1], above[2], above[3]
	e, f, g, h := above[4], above[5], above[6], above[7]

	dst[0*dstStride+0] = avg2(a, b)
	dst[0*dstStride+1] = avg2(b, c)
	dst[2*dstStride+0] = dst[0*dstStride+1]
	dst[0*dstStride+2] = avg2(c, d)
	dst[2*dstStride+1] = dst[0*dstStride+2]
	dst[0*dstStride+3] = avg2(d, e)
	dst[2*dstStride+2] = dst[0*dstStride+3]
	dst[2*dstStride+3] = avg3(e, f, g)

	dst[1*dstStride+0] = avg3(a, b, c)
	dst[1*dstStride+1] = avg3(b, c, d)
	dst[3*dstStride+0] = dst[1*dstStride+1]
	dst[1*dstStride+2] = avg3(c, d, e)
	dst[3*dstStride+1] = dst[1*dstStride+2]
	dst[1*dstStride+3] = avg3(d, e, f)
	dst[3*dstStride+2] = dst[1*dstStride+3]
	dst[3*dstStride+3] = avg3(f, g, h)
}

func intra4x4HDPredictScalar(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	a, b, c := above[0], above[1], above[2]
	i, j, k, l := left[0], left[1], left[2], left[3]
	x := topLeft

	dst[0*dstStride+0] = avg2(i, x)
	dst[1*dstStride+2] = dst[0*dstStride+0]
	dst[1*dstStride+0] = avg2(j, i)
	dst[2*dstStride+2] = dst[1*dstStride+0]
	dst[2*dstStride+0] = avg2(k, j)
	dst[3*dstStride+2] = dst[2*dstStride+0]
	dst[3*dstStride+0] = avg2(l, k)

	dst[0*dstStride+3] = avg3(a, b, c)
	dst[0*dstStride+2] = avg3(x, a, b)
	dst[0*dstStride+1] = avg3(i, x, a)
	dst[1*dstStride+3] = dst[0*dstStride+1]
	dst[1*dstStride+1] = avg3(j, i, x)
	dst[2*dstStride+3] = dst[1*dstStride+1]
	dst[2*dstStride+1] = avg3(k, j, i)
	dst[3*dstStride+3] = dst[2*dstStride+1]
	dst[3*dstStride+1] = avg3(l, k, j)
}

func intra4x4HUPredictScalar(dst []byte, dstStride int, left []byte) {
	i, j, k, l := left[0], left[1], left[2], left[3]

	dst[0*dstStride+0] = avg2(i, j)
	dst[0*dstStride+2] = avg2(j, k)
	dst[1*dstStride+0] = dst[0*dstStride+2]
	dst[1*dstStride+2] = avg2(k, l)
	dst[2*dstStride+0] = dst[1*dstStride+2]
	dst[0*dstStride+1] = avg3(i, j, k)
	dst[0*dstStride+3] = avg3(j, k, l)
	dst[1*dstStride+1] = dst[0*dstStride+3]
	dst[1*dstStride+3] = avg3(k, l, l)
	dst[2*dstStride+1] = dst[1*dstStride+3]
	dst[2*dstStride+3] = l
	dst[2*dstStride+2] = l
	dst[3*dstStride+0] = l
	dst[3*dstStride+1] = l
	dst[3*dstStride+2] = l
	dst[3*dstStride+3] = l
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

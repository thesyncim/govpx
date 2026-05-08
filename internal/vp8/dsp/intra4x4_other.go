//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback for VP8 4x4 intra-prediction primitives. Mirrors
// libvpx v1.16.0 vp8/common/reconintra4x4.c semantics. The amd64/arm64
// build tags carry SIMD-accelerated kernels with byte-identical output.

func intra4x4DCPredict(dst []byte, dstStride int, above []byte, left []byte) {
	intra4x4DCPredictScalar(dst, dstStride, above, left)
}

func intra4x4TMPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4TMPredictScalar(dst, dstStride, above, left, topLeft)
}

func intra4x4VEPredict(dst []byte, dstStride int, above []byte, topLeft byte) {
	intra4x4VEPredictScalar(dst, dstStride, above, topLeft)
}

func intra4x4HEPredict(dst []byte, dstStride int, left []byte, topLeft byte) {
	intra4x4HEPredictScalar(dst, dstStride, left, topLeft)
}

func intra4x4LDPredict(dst []byte, dstStride int, above []byte) {
	intra4x4LDPredictScalar(dst, dstStride, above)
}

func intra4x4RDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4RDPredictScalar(dst, dstStride, above, left, topLeft)
}

func intra4x4VRPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4VRPredictScalar(dst, dstStride, above, left, topLeft)
}

func intra4x4VLPredict(dst []byte, dstStride int, above []byte) {
	intra4x4VLPredictScalar(dst, dstStride, above)
}

func intra4x4HDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4HDPredictScalar(dst, dstStride, above, left, topLeft)
}

func intra4x4HUPredict(dst []byte, dstStride int, left []byte) {
	intra4x4HUPredictScalar(dst, dstStride, left)
}

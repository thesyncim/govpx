package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Shared VP8 4x4 intra-prediction dispatch mirrors libvpx v1.16.0
// vp8/common/reconintra4x4.c, using Go SIMD when enabled.

func intra4x4DCPredict(dst []byte, dstStride int, above []byte, left []byte) {
	if pixel.Enabled {
		if pixel.Intra4x4DCFill(dst, dstStride, above, left) {
			return
		}
		intraDCGo(dst, dstStride, above, left, 4, true, true)
	} else {
		intra4x4DCPredictScalar(dst, dstStride, above, left)
	}
}

func intra4x4TMPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if pixel.Enabled {
		if pixel.TMNative {
			pixel.TMPredict4Native(dst, dstStride, above, left, topLeft, false)
		} else {
			pixel.TMPredict(dst, dstStride, above, left, topLeft, 4)
		}
	} else {
		intra4x4TMPredictScalar(dst, dstStride, above, left, topLeft)
	}
}

func intra4x4VEPredict(dst []byte, dstStride int, above []byte, topLeft byte) {
	if intraDirectionalGo(0, dst, dstStride, above, nil, topLeft) {
		return
	}

	intra4x4VEPredictScalar(dst, dstStride, above, topLeft)
}

func intra4x4HEPredict(dst []byte, dstStride int, left []byte, topLeft byte) {
	if intraDirectionalGo(1, dst, dstStride, nil, left, topLeft) {
		return
	}

	intra4x4HEPredictScalar(dst, dstStride, left, topLeft)
}

func intra4x4LDPredict(dst []byte, dstStride int, above []byte) {
	if intraDirectionalGo(2, dst, dstStride, above, nil, 0) {
		return
	}

	intra4x4LDPredictScalar(dst, dstStride, above)
}

func intra4x4RDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if intraDirectionalGo(3, dst, dstStride, above, left, topLeft) {
		return
	}

	intra4x4RDPredictScalar(dst, dstStride, above, left, topLeft)
}

func intra4x4VRPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if intraDirectionalGo(4, dst, dstStride, above, left, topLeft) {
		return
	}

	intra4x4VRPredictScalar(dst, dstStride, above, left, topLeft)
}

func intra4x4VLPredict(dst []byte, dstStride int, above []byte) {
	if intraDirectionalGo(5, dst, dstStride, above, nil, 0) {
		return
	}

	intra4x4VLPredictScalar(dst, dstStride, above)
}

func intra4x4HDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if intraDirectionalGo(6, dst, dstStride, above, left, topLeft) {
		return
	}

	intra4x4HDPredictScalar(dst, dstStride, above, left, topLeft)
}

func intra4x4HUPredict(dst []byte, dstStride int, left []byte) {
	if intraDirectionalGo(7, dst, dstStride, nil, left, 0) {
		return
	}

	intra4x4HUPredictScalar(dst, dstStride, left)
}

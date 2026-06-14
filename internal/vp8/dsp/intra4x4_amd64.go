//go:build amd64 && !purego

package dsp

import "unsafe"

// amd64 SSE2 dispatch for VP8 4x4 B_PRED intra-prediction kernels.
// Mirrors libvpx v1.16.0 vp8/common/reconintra4x4.c semantics with
// byte-identical output to the scalar reference. SSE2 is part of the
// x86-64 baseline, so the SIMD entry points are always safe to call
// without a runtime feature check.
//
// libvpx's SSE2 paths in the upstream tree handle only the 8x8/16x16
// whole-block predictors; the 4x4 directional kernels run via the
// scalar reference. Keeping the same int16-lane formula here lets us
// avoid the rounding-halving idiom that VP9's NEON predictors use,
// which is not bit-exact for the VP8 reconintra4x4.c AVG3 definition.

//go:noescape
func intra4x4DCPredictSSE2(dst *byte, stride int, above *byte, left *byte)

//go:noescape
func intra4x4TMPredictSSE2(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4VEPredictSSE2(dst *byte, stride int, above *byte, topLeft byte)

//go:noescape
func intra4x4HEPredictSSE2(dst *byte, stride int, left *byte, topLeft byte)

//go:noescape
func intra4x4LDPredictSSE2(dst *byte, stride int, above *byte)

//go:noescape
func intra4x4RDPredictSSE2(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4VRPredictSSE2(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4VLPredictSSE2(dst *byte, stride int, above *byte)

//go:noescape
func intra4x4HDPredictSSE2(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4HUPredictSSE2(dst *byte, stride int, left *byte)

// Each wrapper checks the exact scalar window before handing slice base
// pointers to SSE2 via unsafe.SliceData. Invalid windows deliberately
// fall back to the scalar kernel so package-internal callers keep scalar
// behavior instead of feeding unchecked pointers to assembly.

func intra4x4DCPredict(dst []byte, dstStride int, above []byte, left []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 4) {
		intra4x4DCPredictScalar(dst, dstStride, above, left)
		return
	}
	intra4x4DCPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left))
}

func intra4x4TMPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 4) {
		intra4x4TMPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4TMPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4VEPredict(dst []byte, dstStride int, above []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 5, nil, 0) {
		intra4x4VEPredictScalar(dst, dstStride, above, topLeft)
		return
	}
	intra4x4VEPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), topLeft)
}

func intra4x4HEPredict(dst []byte, dstStride int, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, nil, 0, left, 4) {
		intra4x4HEPredictScalar(dst, dstStride, left, topLeft)
		return
	}
	intra4x4HEPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(left), topLeft)
}

func intra4x4LDPredict(dst []byte, dstStride int, above []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 8, nil, 0) {
		intra4x4LDPredictScalar(dst, dstStride, above)
		return
	}
	intra4x4LDPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above))
}

func intra4x4RDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 4) {
		intra4x4RDPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4RDPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4VRPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 3) {
		intra4x4VRPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4VRPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4VLPredict(dst []byte, dstStride int, above []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 8, nil, 0) {
		intra4x4VLPredictScalar(dst, dstStride, above)
		return
	}
	intra4x4VLPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above))
}

func intra4x4HDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 3, left, 4) {
		intra4x4HDPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4HDPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4HUPredict(dst []byte, dstStride int, left []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, nil, 0, left, 4) {
		intra4x4HUPredictScalar(dst, dstStride, left)
		return
	}
	intra4x4HUPredictSSE2(unsafe.SliceData(dst), dstStride, unsafe.SliceData(left))
}

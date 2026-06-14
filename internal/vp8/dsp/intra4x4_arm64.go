//go:build arm64 && !purego

package dsp

import "unsafe"

// arm64 NEON dispatch for VP8 4x4 B_PRED intra-prediction kernels.
// Mirrors libvpx v1.16.0 vp8/common/reconintra4x4.c semantics with
// byte-identical output to the scalar reference.
//
// The 4x4 size means each kernel emits four 4-byte (one int32) row
// stores. Inputs are 4-8 bytes of context, so we load once into 64-bit
// NEON registers and compute the avg2/avg3 lanes in parallel as int16
// before packing back to bytes.

//go:noescape
func intra4x4DCPredictNEON(dst *byte, stride int, above *byte, left *byte)

//go:noescape
func intra4x4TMPredictNEON(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4VEPredictNEON(dst *byte, stride int, above *byte, topLeft byte)

//go:noescape
func intra4x4HEPredictNEON(dst *byte, stride int, left *byte, topLeft byte)

//go:noescape
func intra4x4LDPredictNEON(dst *byte, stride int, above *byte)

//go:noescape
func intra4x4RDPredictNEON(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4VRPredictNEON(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4VLPredictNEON(dst *byte, stride int, above *byte)

//go:noescape
func intra4x4HDPredictNEON(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intra4x4HUPredictNEON(dst *byte, stride int, left *byte)

// Each wrapper checks the exact scalar window before handing slice base
// pointers to NEON via unsafe.SliceData. Invalid windows deliberately
// fall back to the scalar kernel so package-internal callers keep scalar
// behavior instead of feeding unchecked pointers to assembly.

func intra4x4DCPredict(dst []byte, dstStride int, above []byte, left []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 4) {
		intra4x4DCPredictScalar(dst, dstStride, above, left)
		return
	}
	intra4x4DCPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left))
}

func intra4x4TMPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 4) {
		intra4x4TMPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4TMPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4VEPredict(dst []byte, dstStride int, above []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 5, nil, 0) {
		intra4x4VEPredictScalar(dst, dstStride, above, topLeft)
		return
	}
	intra4x4VEPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), topLeft)
}

func intra4x4HEPredict(dst []byte, dstStride int, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, nil, 0, left, 4) {
		intra4x4HEPredictScalar(dst, dstStride, left, topLeft)
		return
	}
	intra4x4HEPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(left), topLeft)
}

func intra4x4LDPredict(dst []byte, dstStride int, above []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 8, nil, 0) {
		intra4x4LDPredictScalar(dst, dstStride, above)
		return
	}
	intra4x4LDPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above))
}

func intra4x4RDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 4) {
		intra4x4RDPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4RDPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4VRPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 4, left, 3) {
		intra4x4VRPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4VRPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4VLPredict(dst []byte, dstStride int, above []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 8, nil, 0) {
		intra4x4VLPredictScalar(dst, dstStride, above)
		return
	}
	intra4x4VLPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above))
}

func intra4x4HDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, above, 3, left, 4) {
		intra4x4HDPredictScalar(dst, dstStride, above, left, topLeft)
		return
	}
	intra4x4HDPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intra4x4HUPredict(dst []byte, dstStride int, left []byte) {
	if !intra4x4PredictWindowOK(dst, dstStride, nil, 0, left, 4) {
		intra4x4HUPredictScalar(dst, dstStride, left)
		return
	}
	intra4x4HUPredictNEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(left))
}

//go:build arm64

package dsp

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

func intra4x4DCPredict(dst []byte, dstStride int, above []byte, left []byte) {
	intra4x4DCPredictNEON(&dst[0], dstStride, &above[0], &left[0])
}

func intra4x4TMPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4TMPredictNEON(&dst[0], dstStride, &above[0], &left[0], topLeft)
}

func intra4x4VEPredict(dst []byte, dstStride int, above []byte, topLeft byte) {
	intra4x4VEPredictNEON(&dst[0], dstStride, &above[0], topLeft)
}

func intra4x4HEPredict(dst []byte, dstStride int, left []byte, topLeft byte) {
	intra4x4HEPredictNEON(&dst[0], dstStride, &left[0], topLeft)
}

func intra4x4LDPredict(dst []byte, dstStride int, above []byte) {
	intra4x4LDPredictNEON(&dst[0], dstStride, &above[0])
}

func intra4x4RDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4RDPredictNEON(&dst[0], dstStride, &above[0], &left[0], topLeft)
}

func intra4x4VRPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4VRPredictNEON(&dst[0], dstStride, &above[0], &left[0], topLeft)
}

func intra4x4VLPredict(dst []byte, dstStride int, above []byte) {
	intra4x4VLPredictNEON(&dst[0], dstStride, &above[0])
}

func intra4x4HDPredict(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	intra4x4HDPredictNEON(&dst[0], dstStride, &above[0], &left[0], topLeft)
}

func intra4x4HUPredict(dst []byte, dstStride int, left []byte) {
	intra4x4HUPredictNEON(&dst[0], dstStride, &left[0])
}

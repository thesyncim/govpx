//go:build arm64

package dsp

// ARMv8 NEON intra-prediction primitives. Mirrors libvpx v1.16.0
// vp8/common/arm/neon/vp8_intrapred_neon.c per-mode kernels for the
// Y16x16 and UV8x8 whole-block predictors used by the encoder
// (PredictIntraY16x16 / PredictIntraUV8x8) and decoder.

//go:noescape
func intraSum16NEON(src *byte) int32

//go:noescape
func intraSum8NEON(src *byte) int32

//go:noescape
func intraFill16x16NEON(dst *byte, stride int, val byte)

//go:noescape
func intraFill8x8NEON(dst *byte, stride int, val byte)

// V-prediction is left to the scalar `copy(dst, above)` path: at the
// 16-byte (and 8-byte) row size the compiler already lowers it to a
// single MOVOU/STR, so wrapping it in a NEON kernel only adds dispatch
// overhead. Kept here as a comment for completeness.

//go:noescape
func intraHPredict16x16NEON(dst *byte, stride int, left *byte)

//go:noescape
func intraHPredict8x8NEON(dst *byte, stride int, left *byte)

//go:noescape
func intraTMPredict16x16NEON(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intraTMPredict8x8NEON(dst *byte, stride int, above *byte, left *byte, topLeft byte)

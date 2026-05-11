//go:build amd64 && !purego

package dsp

// SSE2 intra-prediction primitives. Mirrors libvpx v1.16.0
// vp8/common/x86/vp8_intrapred_sse2.asm per-mode kernels for the
// Y16x16 and UV8x8 whole-block predictors used by the encoder
// (PredictIntraY16x16 / PredictIntraUV8x8) and decoder.

//go:noescape
func intraSum16SSE2(src *byte) int32

//go:noescape
func intraSum8SSE2(src *byte) int32

//go:noescape
func intraFill16x16SSE2(dst *byte, stride int, val byte)

//go:noescape
func intraFill8x8SSE2(dst *byte, stride int, val byte)

//go:noescape
func intraHPredict16x16SSE2(dst *byte, stride int, left *byte)

//go:noescape
func intraHPredict8x8SSE2(dst *byte, stride int, left *byte)

//go:noescape
func intraTMPredict16x16SSE2(dst *byte, stride int, above *byte, left *byte, topLeft byte)

//go:noescape
func intraTMPredict8x8SSE2(dst *byte, stride int, above *byte, left *byte, topLeft byte)

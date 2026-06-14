//go:build arm64 && !purego

package dsp

import "unsafe"

// NEON port of libvpx v1.16.0 vp8/common/arm/neon/shortidct4x4llm_neon.c
// (vp8_short_idct4x4llm_neon) plus a 4x4 DC-only fast path. Outputs are
// byte-identical to the scalar references for VP8 coefficient ranges.

//go:noescape
func idct4x4AddNEON(input *int16, pred *byte, predStride int, dst *byte, dstStride int)

//go:noescape
func dcOnlyIDCT4x4AddNEON(inputDC int16, pred *byte, predStride int, dst *byte, dstStride int)

func idct4x4AddSIMD(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	if !dspSIMDPredictWindowOK(pred, predStride, 4, 4, dst, dstStride, 4, 4) {
		idct4x4AddScalar(input, pred, predStride, dst, dstStride)
		return
	}
	idct4x4AddNEON(&input[0], unsafe.SliceData(pred), predStride, unsafe.SliceData(dst), dstStride)
}

func dcOnlyIDCT4x4AddSIMD(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	if !dspSIMDPredictWindowOK(pred, predStride, 4, 4, dst, dstStride, 4, 4) {
		dcOnlyIDCT4x4AddScalar(inputDC, pred, predStride, dst, dstStride)
		return
	}
	dcOnlyIDCT4x4AddNEON(inputDC, unsafe.SliceData(pred), predStride, unsafe.SliceData(dst), dstStride)
}

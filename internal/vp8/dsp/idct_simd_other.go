//go:build (!arm64 && !amd64) || purego

package dsp

// Pure-Go fallback dispatchers for the IDCT SIMD entry points. They
// mirror the libvpx v1.16.0 vp8/common/idctllm.c scalar references
// exactly.

func idct4x4AddSIMD(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	idct4x4AddScalar(input, pred, predStride, dst, dstStride)
}

func dcOnlyIDCT4x4AddSIMD(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	dcOnlyIDCT4x4AddScalar(inputDC, pred, predStride, dst, dstStride)
}

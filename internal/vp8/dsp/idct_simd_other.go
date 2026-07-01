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

func dcOnlyIDCT4x4AddPairSIMD(delta0 int16, delta1 int16, pred []byte, predStride int, dst []byte, dstStride int) {
	dcOnlyIDCT4x4AddSIMD(delta0<<3, pred, predStride, dst, dstStride)
	dcOnlyIDCT4x4AddSIMD(delta1<<3, pred[4:], predStride, dst[4:], dstStride)
}

func dequantIDCTAddFull2xSIMD(q *[32]int16, dq *[16]int16, dst []byte, stride int) {
	dequantIDCTAddFull2xFallback(q, dq, dst, stride)
}

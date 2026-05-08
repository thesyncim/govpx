//go:build amd64

package dsp

// IDCT dispatch on amd64. The 4x4 IDCT and DC-only fast path currently
// fall through to the scalar reference; the libvpx v1.16.0 SSE2 IDCT
// (vp8/common/x86/idctllm_sse2.asm) operates on pairs of blocks
// (idct_dequant_*_2x_sse2) and adapting to govpx's single-block API isn't
// a meaningful win compared to the cost of the port. The hot paths on
// amd64 are FastQuantizeBlock and ForwardDCT4x4 which receive SSE2 ports
// in their respective package files.

func idct4x4AddSIMD(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	idct4x4AddScalar(input, pred, predStride, dst, dstStride)
}

func dcOnlyIDCT4x4AddSIMD(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	dcOnlyIDCT4x4AddScalar(inputDC, pred, predStride, dst, dstStride)
}

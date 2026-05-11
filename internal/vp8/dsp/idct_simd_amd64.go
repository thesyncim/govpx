//go:build amd64 && !purego

package dsp

import "unsafe"

// SSE2 ports of libvpx v1.16.0 vp8/common/idctllm.c
// (vp8_short_idct4x4llm_c) and the DC-only fast path. The libvpx SSE2
// reference processes pairs of blocks (vp8_idct_dequant_full_2x_sse2
// in vp8/common/x86/idctllm_sse2.asm); we mirror its butterfly +
// PMULHW MAC approach but on a single block, matching govpx's
// per-block API. Output is byte-identical to the scalar reference for
// VP8 coefficient ranges.

//go:noescape
func idct4x4AddSSE2(input *int16, pred *byte, predStride int, dst *byte, dstStride int)

//go:noescape
func dcOnlyIDCT4x4AddSSE2(inputDC int16, pred *byte, predStride int, dst *byte, dstStride int)

func idct4x4AddSIMD(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	idct4x4AddSSE2(&input[0], unsafe.SliceData(pred), predStride, unsafe.SliceData(dst), dstStride)
}

func dcOnlyIDCT4x4AddSIMD(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	dcOnlyIDCT4x4AddSSE2(inputDC, unsafe.SliceData(pred), predStride, unsafe.SliceData(dst), dstStride)
}

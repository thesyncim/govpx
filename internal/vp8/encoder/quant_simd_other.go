//go:build (!arm64 && !amd64) || purego

package encoder

// Pure-Go fallback dispatchers for the FastQuantizeBlock, ForwardDCT4x4, and
// ForwardWalsh4x4 SIMD entry points. They mirror the libvpx v1.16.0
// vp8/encoder/vp8_quantize.c (vp8_fast_quantize_b_c), vp8/encoder/dct.c
// (vp8_short_fdct4x4_c) and vp8/encoder/dct.c (vp8_short_walsh4x4_c) scalar
// references exactly.

func fastQuantizeBlockSIMD(coeff *[16]int16, quant *BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return fastQuantizeBlockScalar(coeff, quant, qcoeff, dqcoeff)
}

func forwardDCT4x4SIMD(input []int16, stride int, output *[16]int16) {
	forwardDCT4x4Scalar(input, stride, output)
}

func forwardWalsh4x4SIMD(input []int16, stride int, output *[16]int16) {
	forwardWalsh4x4Scalar(input, stride, output)
}

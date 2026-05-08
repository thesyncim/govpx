//go:build amd64

package encoder

// SSE2 port of libvpx v1.16.0 vp8/encoder/x86/vp8_quantize_sse2.c
// vp8_fast_quantize_b_sse2. Output is byte-identical to FastQuantizeBlock
// scalar reference for VP8 coefficient ranges.

//go:noescape
func fastQuantizeBlockSSE2(coeff *int16, round *int16, quantFast *int16, dequant *int16, qcoeff *int16, dqcoeff *int16) int32

func fastQuantizeBlockSIMD(coeff *[16]int16, quant *BlockQuant, qcoeff *[16]int16, dqcoeff *[16]int16) int {
	return int(fastQuantizeBlockSSE2(&coeff[0], &quant.Round[0], &quant.QuantFast[0], &quant.Dequant[0], &qcoeff[0], &dqcoeff[0]))
}

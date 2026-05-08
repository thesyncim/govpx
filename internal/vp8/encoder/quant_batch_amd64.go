//go:build amd64

package encoder

// SSE2 batched port of libvpx v1.16.0
// vp8/encoder/x86/vp8_quantize_sse2.c vp8_fast_quantize_b_sse2.
// Same per-block kernel as fastQuantizeBlockSSE2; the quant tables
// (round / quantFast / dequant) and the inv-zigzag mask are loaded
// once before the loop, mirroring libvpx's vp8_quantize_mby /
// vp8_quantize_mbuv pattern.

//go:noescape
func fastQuantizeBlockBatchSSE2(coeff *int16, round *int16, quantFast *int16, dequant *int16, qcoeff *int16, dqcoeff *int16, eobs *uint8, count int)

func fastQuantizeBlockBatchSIMD(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	if count <= 0 {
		return
	}
	fastQuantizeBlockBatchSSE2(&coeff[0], &quant.Round[0], &quant.QuantFast[0], &quant.Dequant[0], &qcoeff[0], &dqcoeff[0], &eobs[0], count)
}

//go:build !arm64 && !amd64

package encoder

// Pure-Go fallback dispatcher for the batched fast-quantize entry
// point. Mirrors libvpx v1.16.0 vp8/encoder/vp8_quantize.c
// vp8_fast_quantize_b_c per block.

func fastQuantizeBlockBatchSIMD(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	fastQuantizeBlockBatchScalar(coeff, quant, qcoeff, dqcoeff, eobs, count)
}

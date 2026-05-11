//go:build (!arm64 && !amd64) || purego

package encoder

// Pure-Go fallback dispatcher for the batched fast-quantize entry
// point. Mirrors libvpx v1.16.0 vp8/encoder/vp8_quantize.c
// vp8_fast_quantize_b_c per block.

func fastQuantizeBlockBatchSIMD(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	fastQuantizeBlockBatchScalar(coeff, quant, qcoeff, dqcoeff, eobs, count)
}

func fastQuantizeBlockBatchScalar(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	for i := range count {
		// Pointer-cast the per-block slices to 16-element array views so
		// the kernel reads coeff and writes qcoeff/dqcoeff in place,
		// skipping the staging arrays and the three copies per block.
		c := (*[16]int16)(coeff[i*16 : i*16+16])
		q := (*[16]int16)(qcoeff[i*16 : i*16+16])
		dq := (*[16]int16)(dqcoeff[i*16 : i*16+16])
		eobs[i] = uint8(fastQuantizeBlockScalar(c, quant, q, dq))
	}
}

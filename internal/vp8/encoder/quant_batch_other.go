package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Pure-Go fallback dispatcher for the batched fast-quantize entry
// point. Mirrors libvpx v1.16.0 vp8/encoder/vp8_quantize.c
// vp8_fast_quantize_b_c per block.

func fastQuantizeBlockBatchSIMD(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	// Shared-table SIMD amortizes setup for the production eight/16-block batches.
	// Single blocks retain the lower-overhead per-block path.
	if count >= 2 && pixel.QuantizeVP8Batch(coeff, &quant.Round, &quant.QuantFast, &quant.Dequant, qcoeff, dqcoeff, &tables.DefaultInvZigZag, eobs, count) {
		return
	}
	for i := range count {
		eobs[i] = uint8(fastQuantizeBlockSIMD((*[16]int16)(coeff[i*16:]), quant, (*[16]int16)(qcoeff[i*16:]), (*[16]int16)(dqcoeff[i*16:])))
	}
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

func fastQuantizeBlockBatchOnly(coeff []int16, quant *BlockQuant, qcoeff []int16, eobs []uint8, count int) {
	if count >= 2 && pixel.QuantizeVP8BatchOnly(coeff, &quant.Round, &quant.QuantFast, qcoeff, &tables.DefaultInvZigZag, eobs, count) {
		return
	}
	var dq [16]int16
	for i := range count {
		eobs[i] = uint8(fastQuantizeBlockSIMD((*[16]int16)(coeff[i*16:]), quant, (*[16]int16)(qcoeff[i*16:]), &dq))
	}
}

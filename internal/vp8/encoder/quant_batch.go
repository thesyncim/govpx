package encoder

// Whole-MB batched fast-quantize entry point. Mirrors libvpx v1.16.0
// vp8/encoder/vp8_quantize.c vp8_quantize_mby / vp8_quantize_mbuv:
// the 16 Y blocks (or 8 UV blocks) share a single BlockQuant, so the
// per-block kernel can be looped in assembly without re-marshalling
// the quant tables for each block. Per-block output is byte-identical
// to FastQuantizeBlock invoked individually.

// FastQuantizeBlockBatch applies the libvpx fast-quantize kernel to
// `count` consecutive 4x4 coefficient blocks that share the same
// BlockQuant. Inputs and outputs are tightly packed (16 int16 per
// block, eobs is one uint8 per block). Returns nothing — eobs are
// written via the eobs slice in scan order.
//
// The dispatcher hands off to per-arch SIMD ports
// (quant_batch_arm64.go, quant_batch_amd64.go); on platforms without
// a SIMD port it falls through to fastQuantizeBlockBatchScalar, which
// matches the libvpx vp8_fast_quantize_b_c reference per block.
func FastQuantizeBlockBatch(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	if count <= 0 {
		return
	}
	fastQuantizeBlockBatchSIMD(coeff, quant, qcoeff, dqcoeff, eobs, count)
}

func fastQuantizeBlockBatchScalar(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	for i := 0; i < count; i++ {
		var c, q, dq [16]int16
		copy(c[:], coeff[i*16:i*16+16])
		eob := fastQuantizeBlockScalar(&c, quant, &q, &dq)
		copy(qcoeff[i*16:i*16+16], q[:])
		copy(dqcoeff[i*16:i*16+16], dq[:])
		eobs[i] = uint8(eob)
	}
}

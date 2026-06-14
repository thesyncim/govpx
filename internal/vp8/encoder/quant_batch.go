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
// a SIMD port it falls through to the scalar reference in
// quant_batch_other.go, which matches vp8_fast_quantize_b_c per block.
func FastQuantizeBlockBatch(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	if count <= 0 {
		return
	}
	fastQuantizeBlockBatchSIMD(coeff, quant, qcoeff, dqcoeff, eobs, count)
}

func quant4x4BatchWindowOK(coeff []int16, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) bool {
	if count <= 0 {
		return false
	}
	maxInt := int(^uint(0) >> 1)
	if count > maxInt/16 {
		return false
	}
	n := count * 16
	return len(coeff) >= n && len(qcoeff) >= n && len(dqcoeff) >= n && len(eobs) >= count
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

package encoder

// Whole-MB batched fast-quantize entry point. Mirrors libvpx v1.16.0
// vp8/encoder/vp8_quantize.c vp8_quantize_mby / vp8_quantize_mbuv:
// the 16 Y blocks (or 8 UV blocks) share a single BlockQuant, so the
// per-block kernel can retain tables in SIMD registers without re-marshalling
// the quant tables for each block. Per-block output is byte-identical
// to FastQuantizeBlock invoked individually.

// FastQuantizeBlockBatch applies the libvpx fast-quantize kernel to
// `count` consecutive 4x4 coefficient blocks that share the same
// BlockQuant. Inputs and outputs are tightly packed (16 int16 per
// block, eobs is one uint8 per block). Returns nothing — eobs are
// written via the eobs slice in scan order.
//
// Eight-block and larger batches use shared-table Go SIMD on supported ARM
// and x64 CPUs. Smaller batches and unsupported table/layout cases retain
// per-block SIMD or the scalar libvpx v1.16.0 reference.
func FastQuantizeBlockBatch(coeff []int16, quant *BlockQuant, qcoeff []int16, dqcoeff []int16, eobs []uint8, count int) {
	if count <= 0 {
		return
	}
	fastQuantizeBlockBatchSIMD(coeff, quant, qcoeff, dqcoeff, eobs, count)
}

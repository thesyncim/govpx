//go:build amd64 && !purego

package dsp

// SSE2 residual gather used by the encoder's transform-quantize pipeline.
// Mirrors libvpx v1.16.0 vp8/encoder/encodemb.c (vp8_subtract_mby and
// vp8_subtract_mbuv) plus the govpx-specific block-scan reordering done by
// gatherMacroblockYResiduals4x4Unchecked and
// gatherMacroblockUVResiduals4x4Unchecked. Callers have already validated
// that src/pred cover the requested in-bounds macroblock window and that out
// has enough room for the block-major int16 residual slab.

//go:noescape
func residualGather16x16SSE2(src *byte, srcStride int, pred *byte, predStride int, out *int16)

//go:noescape
func residualGather8x8SSE2(src *byte, srcStride int, pred *byte, predStride int, out *int16)

func ResidualGather16x16PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
	residualGather16x16SSE2(src, srcStride, pred, predStride, out)
}

func ResidualGather8x8PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
	residualGather8x8SSE2(src, srcStride, pred, predStride, out)
}

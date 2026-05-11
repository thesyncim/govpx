//go:build arm64

package dsp

// ARMv8 NEON port of the residual gather used by govpx's encoder when it
// writes the 4x4 residuals of a 16x16 luma or 8x8 chroma macroblock into
// the block-major int16 slab consumed by the transform-quantize pipeline.
// Mirrors libvpx v1.16.0 vp8/encoder/encodemb.c (vp8_subtract_mby and
// vp8_subtract_mbuv) plus the govpx-specific block-scan reordering done
// by gatherMacroblockYResiduals4x4Unchecked and
// gatherMacroblockUVResiduals4x4Unchecked.
//
// The kernels assume both src and pred are fully in-bounds for the
// requested 16x16 or 8x8 window; the dispatch wrappers above the gather
// functions already gate on that.

//go:noescape
func residualGather16x16NEON(src *byte, srcStride int, pred *byte, predStride int, out *int16)

//go:noescape
func residualGather8x8NEON(src *byte, srcStride int, pred *byte, predStride int, out *int16)

// ResidualGather16x16PtrFast writes the 16 luma 4x4 residuals of a 16x16
// macroblock into out (16 contiguous int16-per-block slabs in scan order,
// each block laid out row-major at stride 4). The caller must have
// validated that src and pred both cover a 16x16 in-bounds window and
// that out has room for 16*16 int16s.
func ResidualGather16x16PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
	residualGather16x16NEON(src, srcStride, pred, predStride, out)
}

// ResidualGather8x8PtrFast is the chroma analogue: 4 contiguous 4x4
// int16 blocks in scan order (2x2 grid).
func ResidualGather8x8PtrFast(src *byte, srcStride int, pred *byte, predStride int, out *int16) {
	residualGather8x8NEON(src, srcStride, pred, predStride, out)
}

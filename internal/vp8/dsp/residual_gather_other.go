package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Shared residual gather dispatch. Mirrors libvpx v1.16.0
// vp8/encoder/encodemb.c (vp8_subtract_mby / vp8_subtract_mbuv) plus the
// govpx-specific block-scan reordering done by
// gatherMacroblockYResiduals4x4Unchecked and
// gatherMacroblockUVResiduals4x4Unchecked.

// ResidualGather16x16Fast reads full 16x16 blocks at the supplied
// strides and writes 256 coefficients in 4x4 block-scan order.
func ResidualGather16x16Fast(src []byte, srcStride int, pred []byte, predStride int, out []int16) {
	if pixel.Gather16Fast(src, srcStride, pred, predStride, out) {
		return
	}
	if pixel.Enabled {
		pixel.Gather(src, srcStride, pred, predStride, out, 16)
		return
	}
	_ = src[15*srcStride+15]
	_ = pred[15*predStride+15]
	_ = out[255]
	for by := range 4 {
		for bx := range 4 {
			blockOff := (by*4 + bx) * 16
			srcOff := by*4*srcStride + bx*4
			predOff := by*4*predStride + bx*4
			for r := range 4 {
				dstRow := out[blockOff+r*4:]
				srcRow := src[srcOff+r*srcStride:]
				predRow := pred[predOff+r*predStride:]
				dstRow[0] = int16(srcRow[0]) - int16(predRow[0])
				dstRow[1] = int16(srcRow[1]) - int16(predRow[1])
				dstRow[2] = int16(srcRow[2]) - int16(predRow[2])
				dstRow[3] = int16(srcRow[3]) - int16(predRow[3])
			}
		}
	}
}

// ResidualGather8x8Fast gathers an 8x8 chroma block into 64 coefficients.
func ResidualGather8x8Fast(src []byte, srcStride int, pred []byte, predStride int, out []int16) {
	if pixel.Gather8Fast(src, srcStride, pred, predStride, out) {
		return
	}
	if pixel.Enabled {
		pixel.Gather(src, srcStride, pred, predStride, out, 8)
		return
	}
	_ = src[7*srcStride+7]
	_ = pred[7*predStride+7]
	_ = out[63]
	for by := range 2 {
		for bx := range 2 {
			blockOff := (by*2 + bx) * 16
			srcOff := by*4*srcStride + bx*4
			predOff := by*4*predStride + bx*4
			for r := range 4 {
				dstRow := out[blockOff+r*4:]
				srcRow := src[srcOff+r*srcStride:]
				predRow := pred[predOff+r*predStride:]
				dstRow[0] = int16(srcRow[0]) - int16(predRow[0])
				dstRow[1] = int16(srcRow[1]) - int16(predRow[1])
				dstRow[2] = int16(srcRow[2]) - int16(predRow[2])
				dstRow[3] = int16(srcRow[3]) - int16(predRow[3])
			}
		}
	}
}

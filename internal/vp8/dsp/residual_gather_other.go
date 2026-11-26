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
	si, pi := 0, 0
	for by := range 4 {
		b0 := by * 64
		for r := range 4 {
			sr := src[si : si+16]
			pr := pred[pi : pi+16]
			_ = sr[15]
			_ = pr[15]
			o0 := out[b0+r*4 : b0+r*4+4]
			o0[0] = int16(sr[0]) - int16(pr[0])
			o0[1] = int16(sr[1]) - int16(pr[1])
			o0[2] = int16(sr[2]) - int16(pr[2])
			o0[3] = int16(sr[3]) - int16(pr[3])
			o1 := out[b0+16+r*4 : b0+16+r*4+4]
			o1[0] = int16(sr[4]) - int16(pr[4])
			o1[1] = int16(sr[5]) - int16(pr[5])
			o1[2] = int16(sr[6]) - int16(pr[6])
			o1[3] = int16(sr[7]) - int16(pr[7])
			o2 := out[b0+32+r*4 : b0+32+r*4+4]
			o2[0] = int16(sr[8]) - int16(pr[8])
			o2[1] = int16(sr[9]) - int16(pr[9])
			o2[2] = int16(sr[10]) - int16(pr[10])
			o2[3] = int16(sr[11]) - int16(pr[11])
			o3 := out[b0+48+r*4 : b0+48+r*4+4]
			o3[0] = int16(sr[12]) - int16(pr[12])
			o3[1] = int16(sr[13]) - int16(pr[13])
			o3[2] = int16(sr[14]) - int16(pr[14])
			o3[3] = int16(sr[15]) - int16(pr[15])
			si += srcStride
			pi += predStride
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
	si, pi := 0, 0
	for by := range 2 {
		b0 := by * 32
		for r := range 4 {
			sr := src[si : si+8]
			pr := pred[pi : pi+8]
			_ = sr[7]
			_ = pr[7]
			o0 := out[b0+r*4 : b0+r*4+4]
			o0[0] = int16(sr[0]) - int16(pr[0])
			o0[1] = int16(sr[1]) - int16(pr[1])
			o0[2] = int16(sr[2]) - int16(pr[2])
			o0[3] = int16(sr[3]) - int16(pr[3])
			o1 := out[b0+16+r*4 : b0+16+r*4+4]
			o1[0] = int16(sr[4]) - int16(pr[4])
			o1[1] = int16(sr[5]) - int16(pr[5])
			o1[2] = int16(sr[6]) - int16(pr[6])
			o1[3] = int16(sr[7]) - int16(pr[7])
			si += srcStride
			pi += predStride
		}
	}
}

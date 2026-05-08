//go:build arm64

package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// ARMv8 NEON port of the libvpx v1.16.0 vp8/common/filter.c bilinear
// (two-tap) subpel predictor. Routes BilinearPredict16x16 and
// BilinearPredict8x8 (the dominant sizes used by VP8's bilinear
// inter-prediction filter when the encoder selects bilinear) through
// hand-written NEON; every other size falls through to the scalar
// reference in subpixel.go.

//go:noescape
func bilinearPredict16x16NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[2]int16, vFilter *[2]int16, tmp *[17 * 16]byte)

//go:noescape
func bilinearPredict8x8NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[2]int16, vFilter *[2]int16, tmp *[9 * 8]byte)

func bilinearPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [17 * 16]byte
	hFilter := &tables.BilinearFilters[xoffset]
	vFilter := &tables.BilinearFilters[yoffset]
	bilinearPredict16x16NEON(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
	return true
}

func bilinearPredict8x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [9 * 8]byte
	hFilter := &tables.BilinearFilters[xoffset]
	vFilter := &tables.BilinearFilters[yoffset]
	bilinearPredict8x8NEON(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
	return true
}

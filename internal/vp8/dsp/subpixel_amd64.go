//go:build amd64

package dsp

import (
	"github.com/thesyncim/govpx/internal/cpu"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// SSE2 port of the libvpx v1.16.0 VP8 six-tap subpel predictor.
// Routes SixTapPredict16x16, SixTapPredict8x8, SixTapPredict8x4,
// SixTapPredict4x4 through hand-written SSE2; the 16x8 and 8x16
// sizes still fall through to the scalar reference in subpixel.go.
//
// The kernel decomposes the 6-tap horizontal/vertical inner product
// into three PMADDWD pairs over byte sources widened to int16 lanes
// — saturated signed pack to int16, then unsigned pack to uint8.
//
// The 16x16 size also has an AVX2 entry point in subpixel_avx2_amd64.s
// that processes 16 columns per iteration in YMM accumulators; gated
// at runtime via internal/cpu.HasAVX2.

//go:noescape
func sixTapPredict16x16SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 16]byte)

//go:noescape
func sixTapPredict16x16AVX2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 16]byte)

//go:noescape
func sixTapPredict8x8SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[13 * 8]byte)

//go:noescape
func sixTapPredict8x4SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[9 * 8]byte)

//go:noescape
func sixTapPredict4x4SSE2(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[9 * 4]byte)

func sixTapPredict16x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [21 * 16]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	if cpu.HasAVX2 {
		sixTapPredict16x16AVX2(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
		return true
	}
	sixTapPredict16x16SSE2(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [13 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x8SSE2(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x4Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [9 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x4SSE2(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict4x4Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [9 * 4]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict4x4SSE2(&dst[0], dstStride, &src[0], srcStride, hFilter, vFilter, &tmp)
	return true
}

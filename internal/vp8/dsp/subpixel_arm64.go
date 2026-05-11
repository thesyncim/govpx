//go:build arm64

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// ARMv8 NEON port of the libvpx v1.16.0
// vp8/common/arm/neon/sixtappredict_neon.c 16x16, 16x8, 8x16, 8x8,
// 8x4, and 4x4 paths.

//go:noescape
func sixTapPredict16x16NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 16]byte)

//go:noescape
func sixTapPredict16x8NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[13 * 16]byte)

//go:noescape
func sixTapPredict8x16NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[21 * 8]byte)

//go:noescape
func sixTapPredict8x8NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[13 * 8]byte)

//go:noescape
func sixTapPredict8x4NEON(dst *byte, dstStride int, src *byte, srcStride int,
	hFilter *[6]int16, vFilter *[6]int16, tmp *[9 * 8]byte)

//go:noescape
func sixTapPredict4x4NEON(dst *byte, dstStride int, src *byte, srcStride int,
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
	sixTapPredict16x16NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict16x8Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [13 * 16]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict16x8NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x16Maybe(src []byte, srcStride int, xoffset int, yoffset int,
	dst []byte, dstStride int) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if srcStride <= 0 || dstStride <= 0 {
		return false
	}
	var tmp [21 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x16NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
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
	sixTapPredict8x8NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

func sixTapPredict8x8PairMaybe(
	src0 []byte, src0Stride int,
	src1 []byte, src1Stride int,
	xoffset int, yoffset int,
	dst0 []byte, dst0Stride int,
	dst1 []byte, dst1Stride int,
) bool {
	if xoffset < 0 || xoffset >= 8 || yoffset < 0 || yoffset >= 8 {
		return false
	}
	if src0Stride <= 0 || src1Stride <= 0 || dst0Stride <= 0 || dst1Stride <= 0 {
		return false
	}
	var tmp [13 * 8]byte
	hFilter := &tables.SubPelFilters[xoffset]
	vFilter := &tables.SubPelFilters[yoffset]
	sixTapPredict8x8NEON(unsafe.SliceData(dst0), dst0Stride, unsafe.SliceData(src0), src0Stride, hFilter, vFilter, &tmp)
	sixTapPredict8x8NEON(unsafe.SliceData(dst1), dst1Stride, unsafe.SliceData(src1), src1Stride, hFilter, vFilter, &tmp)
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
	sixTapPredict8x4NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
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
	sixTapPredict4x4NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(src), srcStride, hFilter, vFilter, &tmp)
	return true
}

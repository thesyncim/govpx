//go:build arm64 && !purego

package dsp

import (
	"unsafe"
)

// Ported from libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c
// var_filter_block2d_bil_w16 specialized for byte output.

//go:noescape
func bilinearFilter16x16HorizontalNEON(src *byte, srcStride int, dst *byte, height int, f0 uint64, f1 uint64)

//go:noescape
func bilinearFilter16x16VerticalNEON(src *byte, srcStride int, dst *byte, height int, f0 uint64, f1 uint64)

func bilinearFilter16x16Horizontal(src []byte, srcStride int, dst []byte, height int, filter [2]int16) {
	bilinearFilter16x16HorizontalNEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(dst), height, uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}

func bilinearFilter16x16Vertical(src []byte, srcStride int, dst []byte, height int, filter [2]int16) {
	bilinearFilter16x16VerticalNEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(dst), height, uint64(uint16(filter[0])), uint64(uint16(filter[1])))
}

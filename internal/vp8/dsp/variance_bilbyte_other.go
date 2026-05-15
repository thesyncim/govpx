//go:build !arm64 || purego

package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c
// var_filter_block2d_bil_w16 specialized for byte output.

func bilinearFilter16x16Horizontal(src []byte, srcStride int, dst []byte, height int, filter [2]int16) {
	for y := range height {
		srcRow := y * srcStride
		dstRow := y * 16
		for x := range 16 {
			v := int(src[srcRow+x])*int(filter[0]) + int(src[srcRow+x+1])*int(filter[1])
			dst[dstRow+x] = byte((v + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}
}

func bilinearFilter16x16Vertical(src []byte, srcStride int, dst []byte, height int, filter [2]int16) {
	for y := range height {
		srcRow := y * srcStride
		dstRow := y * 16
		for x := range 16 {
			v := int(src[srcRow+x])*int(filter[0]) + int(src[srcRow+srcStride+x])*int(filter[1])
			dst[dstRow+x] = byte((v + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}
}

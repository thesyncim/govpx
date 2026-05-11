//go:build (!arm64 && !amd64) || purego

package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Pure-Go fallbacks for the width-8 / width-4 bilinear-filter subpel
// kernels used by SubpelVariance{8x16,8x8,8x4,4x8,4x4}. Mirrors
// libvpx v1.16.0 vpx_dsp/variance.c.

func varFilterBlock2DBilinearFirstPass8(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	bilinearFirstPassScalar(src, srcStride, dst, 8, height, filter)
}

func varFilterBlock2DBilinearFirstPass4(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	bilinearFirstPassScalar(src, srcStride, dst, 4, height, filter)
}

func varFilterBlock2DBilinearSecondPass8(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	bilinearSecondPassScalar(src, dst, 8, height, filter)
}

func varFilterBlock2DBilinearSecondPass4(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	bilinearSecondPassScalar(src, dst, 4, height, filter)
}

func bilinearFirstPassScalar(src []byte, srcStride int, dst *[17 * 16]uint16,
	width, height int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := 0; y < height; y++ {
		srcRow := y * srcStride
		dstRow := y * width
		for x := 0; x < width; x++ {
			v := int(src[srcRow+x])*f0 + int(src[srcRow+x+1])*f1
			dst[dstRow+x] = uint16((v + round) >> shift)
		}
	}
}

func bilinearSecondPassScalar(src *[17 * 16]uint16, dst []byte,
	width, height int, filter [2]int16) {
	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := 0; y < height; y++ {
		srcRow := y * width
		dstRow := y * width
		for x := 0; x < width; x++ {
			v := int(src[srcRow+x])*f0 + int(src[srcRow+x+width])*f1
			dst[dstRow+x] = byte((v + round) >> shift)
		}
	}
}

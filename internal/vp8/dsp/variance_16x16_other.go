package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Pure-Go fallback for the 16-wide second-pass bilinear filter on
// architectures without a NEON port. Mirrors libvpx v1.16.0
// vpx_dsp/variance.c semantics via varFilterBlock2DBilinearSecondPass16Scalar.

func varFilterBlock2DBilinearSecondPass16(src *[17 * 16]uint16, dst []byte, height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.BilinearSecond(src[:], dst, 16, height, filter)
		return
	}

	varFilterBlock2DBilinearSecondPass16Scalar(src, dst, height, filter)
}

// Ported from libvpx v1.16.0 vpx_dsp/arm/subpel_variance_neon.c
// var_filter_block2d_bil_w16 specialized for byte output.

func bilinearFilter16x16Horizontal(src []byte, srcStride int, dst []byte, height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.Filter(src, srcStride, dst, 16, 16, height, 1, filter[:])
		return
	}

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
	if pixel.Enabled {
		pixel.Filter(src, srcStride, dst, 16, 16, height, srcStride, filter[:])
		return
	}

	for y := range height {
		srcRow := y * srcStride
		dstRow := y * 16
		for x := range 16 {
			v := int(src[srcRow+x])*int(filter[0]) + int(src[srcRow+srcStride+x])*int(filter[1])
			dst[dstRow+x] = byte((v + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}
}

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	sum, sse := pixel.Stats(src, srcStride, ref, refStride, 16, 16)
	return sum, int(sse)
}

// VarianceBlock16x16Fast computes 16x16 sum and SSE directly.
func VarianceBlock16x16Fast(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	sum, sse := pixel.Stats(src, srcStride, ref, refStride, 16, 16)
	return sum, int(sse)
}

func varianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, width, height int) (int, int) {
	if pixel.Enabled {
		sum, sse := pixel.Stats(src, srcStride, ref, refStride, width, height)
		return sum, int(sse)
	}

	switch width {
	case 16:
		return varianceBlock16xNScalar(src, srcStride, ref, refStride, height)
	case 8:
		return varianceBlock8xNScalar(src, srcStride, ref, refStride, height)
	case 4:
		return varianceBlock4xNScalar(src, srcStride, ref, refStride, height)
	default:
		return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
	}
}

func SSE16xNFast(src []byte, srcStride int, ref []byte, refStride int, height int) int {
	return int(pixel.SSE16(src, srcStride, ref, refStride, height))
}

func varianceBlock16xNScalar(src []byte, srcStride int, ref []byte, refStride int, height int) (int, int) {
	_ = src[(height-1)*srcStride+15]
	_ = ref[(height-1)*refStride+15]
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		d0 := int(srcRow[0]) - int(refRow[0])
		d1 := int(srcRow[1]) - int(refRow[1])
		d2 := int(srcRow[2]) - int(refRow[2])
		d3 := int(srcRow[3]) - int(refRow[3])
		d4 := int(srcRow[4]) - int(refRow[4])
		d5 := int(srcRow[5]) - int(refRow[5])
		d6 := int(srcRow[6]) - int(refRow[6])
		d7 := int(srcRow[7]) - int(refRow[7])
		d8 := int(srcRow[8]) - int(refRow[8])
		d9 := int(srcRow[9]) - int(refRow[9])
		d10 := int(srcRow[10]) - int(refRow[10])
		d11 := int(srcRow[11]) - int(refRow[11])
		d12 := int(srcRow[12]) - int(refRow[12])
		d13 := int(srcRow[13]) - int(refRow[13])
		d14 := int(srcRow[14]) - int(refRow[14])
		d15 := int(srcRow[15]) - int(refRow[15])
		sum += d0 + d1 + d2 + d3 + d4 + d5 + d6 + d7 + d8 + d9 + d10 + d11 + d12 + d13 + d14 + d15
		sse += d0*d0 + d1*d1 + d2*d2 + d3*d3 + d4*d4 + d5*d5 + d6*d6 + d7*d7 +
			d8*d8 + d9*d9 + d10*d10 + d11*d11 + d12*d12 + d13*d13 + d14*d14 + d15*d15
	}
	return sum, sse
}

func varianceBlock8xNScalar(src []byte, srcStride int, ref []byte, refStride int, height int) (int, int) {
	_ = src[(height-1)*srcStride+7]
	_ = ref[(height-1)*refStride+7]
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		d0 := int(srcRow[0]) - int(refRow[0])
		d1 := int(srcRow[1]) - int(refRow[1])
		d2 := int(srcRow[2]) - int(refRow[2])
		d3 := int(srcRow[3]) - int(refRow[3])
		d4 := int(srcRow[4]) - int(refRow[4])
		d5 := int(srcRow[5]) - int(refRow[5])
		d6 := int(srcRow[6]) - int(refRow[6])
		d7 := int(srcRow[7]) - int(refRow[7])
		sum += d0 + d1 + d2 + d3 + d4 + d5 + d6 + d7
		sse += d0*d0 + d1*d1 + d2*d2 + d3*d3 + d4*d4 + d5*d5 + d6*d6 + d7*d7
	}
	return sum, sse
}

func varianceBlock4xNScalar(src []byte, srcStride int, ref []byte, refStride int, height int) (int, int) {
	_ = src[(height-1)*srcStride+3]
	_ = ref[(height-1)*refStride+3]
	sum := 0
	sse := 0
	for y := 0; y < height; y++ {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		d0 := int(srcRow[0]) - int(refRow[0])
		d1 := int(srcRow[1]) - int(refRow[1])
		d2 := int(srcRow[2]) - int(refRow[2])
		d3 := int(srcRow[3]) - int(refRow[3])
		sum += d0 + d1 + d2 + d3
		sse += d0*d0 + d1*d1 + d2*d2 + d3*d3
	}
	return sum, sse
}

// Pure-Go fallback for the 16-wide first-pass bilinear filter on
// architectures without a NEON port. Mirrors libvpx v1.16.0
// vpx_dsp/variance.c semantics.

func varFilterBlock2DBilinearFirstPass16(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.BilinearFirst(src, srcStride, dst[:], 16, height, filter)
		return
	}

	f0 := int(filter[0])
	f1 := int(filter[1])
	const round = tables.FilterWeight / 2
	const shift = tables.FilterShift
	for y := 0; y < height; y++ {
		srcRow := y * srcStride
		dstRow := y * 16
		for x := 0; x < 16; x++ {
			v := int(src[srcRow+x])*f0 + int(src[srcRow+x+1])*f1
			dst[dstRow+x] = uint16((v + round) >> shift)
		}
	}
}

// libvpx v1.16.0 baseline: keep unmeasured non-ARM64 and purego builds on
// the generic two-pass subpel variance path until they have local benchmarks.
const useDirectOneAxisSubpelVariance = false

func subpelVariance16x16Horizontal(src []byte, ss, x int, ref []byte, rs int) (int, int, bool) {
	if !pixel.Enabled {
		return 0, 0, false
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[x], tables.BilinearFilters[0])
	return sum, int(sse), true
}

func subpelVariance16x16Vertical(src []byte, ss, y int, ref []byte, rs int) (int, int, bool) {
	if !pixel.Enabled {
		return 0, 0, false
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[0], tables.BilinearFilters[y])
	return sum, int(sse), true
}

func subpelVariance16x16Bilinear(src []byte, ss, x, y int, ref []byte, rs int) (int, int, bool) {
	if !pixel.Enabled {
		return 0, 0, false
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[x], tables.BilinearFilters[y])
	return sum, int(sse), true
}

func subpelVariance16x16Fast(src []byte, ss, x, y int, ref []byte, rs int) (int, int) {
	if !pixel.Enabled {
		return SubpelVariance16x16(src, ss, x, y, ref, rs)
	}
	sum, sse := pixel.SubpelStats(src, ss, ref, rs, 16, 16, tables.BilinearFilters[x], tables.BilinearFilters[y])
	return int(sse) - sum*sum/256, int(sse)
}

// Pure-Go fallbacks for the width-8 / width-4 bilinear-filter subpel
// kernels used by SubpelVariance{8x16,8x8,8x4,4x8,4x4}. Mirrors
// libvpx v1.16.0 vpx_dsp/variance.c.

func varFilterBlock2DBilinearFirstPass8(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.BilinearFirst(src, srcStride, dst[:], 8, height, filter)
		return
	}

	bilinearFirstPassScalar(src, srcStride, dst, 8, height, filter)
}

func varFilterBlock2DBilinearFirstPass4(src []byte, srcStride int,
	dst *[17 * 16]uint16, height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.BilinearFirst(src, srcStride, dst[:], 4, height, filter)
		return
	}

	bilinearFirstPassScalar(src, srcStride, dst, 4, height, filter)
}

func varFilterBlock2DBilinearSecondPass8(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.BilinearSecond(src[:], dst, 8, height, filter)
		return
	}

	bilinearSecondPassScalar(src, dst, 8, height, filter)
}

func varFilterBlock2DBilinearSecondPass4(src *[17 * 16]uint16, dst []byte,
	height int, filter [2]int16) {
	if pixel.Enabled {
		pixel.BilinearSecond(src[:], dst, 4, height, filter)
		return
	}

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

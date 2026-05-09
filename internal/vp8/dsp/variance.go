package dsp

import "github.com/thesyncim/govpx/internal/vp8/tables"

// Ported from libvpx v1.16.0 vp8/encoder/variance.c scalar variance
// primitives and vpx_dsp/variance.c sub-pixel variance primitives.

func SSE16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 16, 16)
	return sse
}

func SSE16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 16, 8)
	return sse
}

func SSE8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 8, 16)
	return sse
}

func SSE8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 8, 8)
	return sse
}

func SSE8x4(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 8, 4)
	return sse
}

func SSE4x8(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 4, 8)
	return sse
}

func SSE4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	_, sse := varianceBlock(src, srcStride, ref, refStride, 4, 4)
	return sse
}

func Variance16x16(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 16, 16)
	return sse - (sum * sum >> 8)
}

func Variance16x8(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 16, 8)
	return sse - (sum * sum >> 7)
}

func Variance8x16(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 8, 16)
	return sse - (sum * sum >> 7)
}

func Variance8x8(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 8, 8)
	return sse - (sum * sum >> 6)
}

func Variance8x4(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 8, 4)
	return sse - (sum * sum >> 5)
}

func Variance4x8(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 4, 8)
	return sse - (sum * sum >> 5)
}

func Variance4x4(src []byte, srcStride int, ref []byte, refStride int) int {
	sum, sse := varianceBlock(src, srcStride, ref, refStride, 4, 4)
	return sse - (sum * sum >> 4)
}

func SubpelVariance16x16(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int) {
	return subpelVariance(src, srcStride, xOffset, yOffset, ref, refStride, 16, 16)
}

func SubpelVariance16x8(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int) {
	return subpelVariance(src, srcStride, xOffset, yOffset, ref, refStride, 16, 8)
}

func SubpelVariance8x16(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int) {
	return subpelVariance(src, srcStride, xOffset, yOffset, ref, refStride, 8, 16)
}

func SubpelVariance8x8(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int) {
	return subpelVariance(src, srcStride, xOffset, yOffset, ref, refStride, 8, 8)
}

func SubpelVariance8x4(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int) {
	return subpelVariance(src, srcStride, xOffset, yOffset, ref, refStride, 8, 4)
}

func SubpelVariance4x8(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int) {
	return subpelVariance(src, srcStride, xOffset, yOffset, ref, refStride, 4, 8)
}

func SubpelVariance4x4(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int) {
	return subpelVariance(src, srcStride, xOffset, yOffset, ref, refStride, 4, 4)
}

func varianceBlock(src []byte, srcStride int, ref []byte, refStride int, width int, height int) (int, int) {
	if width == 16 && height == 16 {
		return varianceBlock16x16(src, srcStride, ref, refStride)
	}
	if (width == 16 || width == 8 || width == 4) && height > 0 {
		return varianceBlockSized(src, srcStride, ref, refStride, width, height)
	}
	return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
}

// varianceBlockGeneric is the size-agnostic scalar fallback used by
// varianceBlock when the width is not in {4, 8, 16} or height is zero.
// Tests also reference it directly as the parity oracle.
func varianceBlockGeneric(src []byte, srcStride int, ref []byte, refStride int, width int, height int) (int, int) {
	sum := 0
	sse := 0
	for y := range height {
		srcRow := src[y*srcStride:]
		refRow := ref[y*refStride:]
		for x := range width {
			diff := int(srcRow[x]) - int(refRow[x])
			sum += diff
			sse += diff * diff
		}
	}
	return sum, sse
}

func subpelVariance(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int, width int, height int) (int, int) {
	if xOffset == 0 && yOffset == 0 {
		// Bilinear with filter[0]={128,0} reduces to the identity, so the
		// staging passes (and the [17*16]+[16*16] zero-init buffers in this
		// frame) collapse to a direct variance read. The picker / sub-pel
		// diamond hammers the integer-pel centre point so this branch shaves
		// the bilinear flat-time off the hot path while preserving the exact
		// (sse - sum*sum/N, sse) contract.
		sum, sse := varianceBlock(src, srcStride, ref, refStride, width, height)
		return sse - sum*sum/(width*height), sse
	}
	var firstPass [17 * 16]uint16
	var filtered [16 * 16]byte

	varFilterBlock2DBilinearFirstPass(src, srcStride, &firstPass, width, height+1, tables.BilinearFilters[xOffset])
	varFilterBlock2DBilinearSecondPass(&firstPass, filtered[:], width, width, height, width, tables.BilinearFilters[yOffset])
	sum, sse := varianceBlock(filtered[:], width, ref, refStride, width, height)
	return sse - sum*sum/(width*height), sse
}

func varFilterBlock2DBilinearFirstPass(src []byte, srcStride int, dst *[17 * 16]uint16, width int, height int, filter [2]int16) {
	switch width {
	case 16:
		varFilterBlock2DBilinearFirstPass16(src, srcStride, dst, height, filter)
		return
	case 8:
		varFilterBlock2DBilinearFirstPass8(src, srcStride, dst, height, filter)
		return
	case 4:
		varFilterBlock2DBilinearFirstPass4(src, srcStride, dst, height, filter)
		return
	}
	for y := range height {
		srcRow := y * srcStride
		dstRow := y * width
		for x := range width {
			v := int(src[srcRow+x])*int(filter[0]) + int(src[srcRow+x+1])*int(filter[1])
			dst[dstRow+x] = uint16((v + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}
}

func varFilterBlock2DBilinearSecondPass(src *[17 * 16]uint16, dst []byte, srcStride int, pixelStep int, height int, width int, filter [2]int16) {
	if srcStride == width && pixelStep == width {
		switch width {
		case 16:
			varFilterBlock2DBilinearSecondPass16(src, dst, height, filter)
			return
		case 8:
			varFilterBlock2DBilinearSecondPass8(src, dst, height, filter)
			return
		case 4:
			varFilterBlock2DBilinearSecondPass4(src, dst, height, filter)
			return
		}
	}
	for y := range height {
		srcRow := y * srcStride
		dstRow := y * width
		for x := range width {
			v := int(src[srcRow+x])*int(filter[0]) + int(src[srcRow+x+pixelStep])*int(filter[1])
			dst[dstRow+x] = byte((v + tables.FilterWeight/2) >> tables.FilterShift)
		}
	}
}

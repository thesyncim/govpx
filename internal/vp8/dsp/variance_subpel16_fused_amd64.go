//go:build amd64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// AMD64 routes the 16x16 sub-pixel variance hook through the existing
// 16-wide SSE2 bilinear stages plus the 16x16 variance kernel. This is not as
// tight as the arm64 fused NEON kernels, but it avoids the byte-domain scalar
// filter fallback on the hot 16x16 motion-search path.

func subpelVariance16x16Horizontal(src []byte, srcStride int, xOffset int, ref []byte, refStride int) (int, int, bool) {
	sum, sse := subpelVariance16x16Staged(unsafe.SliceData(src), srcStride,
		tables.BilinearFilters[xOffset], tables.BilinearFilters[0],
		unsafe.SliceData(ref), refStride)
	return sum, sse, true
}

func subpelVariance16x16Vertical(src []byte, srcStride int, yOffset int, ref []byte, refStride int) (int, int, bool) {
	sum, sse := subpelVariance16x16Staged(unsafe.SliceData(src), srcStride,
		tables.BilinearFilters[0], tables.BilinearFilters[yOffset],
		unsafe.SliceData(ref), refStride)
	return sum, sse, true
}

func subpelVariance16x16Bilinear(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int, bool) {
	sum, sse := subpelVariance16x16Staged(unsafe.SliceData(src), srcStride,
		tables.BilinearFilters[xOffset], tables.BilinearFilters[yOffset],
		unsafe.SliceData(ref), refStride)
	return sum, sse, true
}

func subpelVariance16x16PtrFast(src *byte, srcStride int, xOffset int, yOffset int, ref *byte, refStride int) (int, int) {
	if xOffset == 0 && yOffset == 0 {
		sum, sse := VarianceBlock16x16PtrFast(src, srcStride, ref, refStride)
		return sse - (sum * sum >> 8), sse
	}
	sum, sse := subpelVariance16x16Staged(src, srcStride,
		tables.BilinearFilters[xOffset], tables.BilinearFilters[yOffset],
		ref, refStride)
	return sse - (sum * sum >> 8), sse
}

func subpelVariance16x16Staged(src *byte, srcStride int, xFilter, yFilter [2]int16, ref *byte, refStride int) (int, int) {
	var firstPass [17 * 16]uint16
	var filtered [16 * 16]byte

	varFilterBlock2DBilinearFirstPass16(unsafe.Slice(src, 16*srcStride+17), srcStride, &firstPass, 17, xFilter)
	varFilterBlock2DBilinearSecondPass16(&firstPass, filtered[:], 16, yFilter)
	return varianceBlock16x16(filtered[:], 16, unsafe.Slice(ref, 15*refStride+16), refStride)
}

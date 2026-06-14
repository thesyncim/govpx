//go:build arm64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Fused 16x16 sub-pixel variance kernels mirror libvpx v1.16.0
// vpx_dsp/arm/subpel_variance_neon.c bilinear filtering and
// vpx_dsp/arm/variance_neon.c variance accumulation.

//go:noescape
func subpelVariance16x16BilinearNEON(src *byte, srcStride int, ref *byte, refStride int, x0 uint64, x1 uint64, y0 uint64, y1 uint64, sumOut *int32, sseOut *uint32)

//go:noescape
func subpelVariance16x16HorizontalNEON(src *byte, srcStride int, ref *byte, refStride int, f0 uint64, f1 uint64, sumOut *int32, sseOut *uint32)

//go:noescape
func subpelVariance16x16VerticalNEON(src *byte, srcStride int, ref *byte, refStride int, f0 uint64, f1 uint64, sumOut *int32, sseOut *uint32)

func subpelVariance16x16Horizontal(src []byte, srcStride int, xOffset int, ref []byte, refStride int) (int, int, bool) {
	filter := tables.BilinearFilters[xOffset]
	if !dspSIMDPredictWindowOK(src, srcStride, 32, 16, ref, refStride, 16, 16) {
		return 0, 0, false
	}
	var sum int32
	var sse uint32
	subpelVariance16x16HorizontalNEON(
		unsafe.SliceData(src),
		srcStride,
		unsafe.SliceData(ref),
		refStride,
		uint64(uint16(filter[0])),
		uint64(uint16(filter[1])),
		&sum,
		&sse,
	)
	return int(sum), int(sse), true
}

func subpelVariance16x16Vertical(src []byte, srcStride int, yOffset int, ref []byte, refStride int) (int, int, bool) {
	filter := tables.BilinearFilters[yOffset]
	if !dspSIMDPredictWindowOK(src, srcStride, 16, 17, ref, refStride, 16, 16) {
		return 0, 0, false
	}
	var sum int32
	var sse uint32
	subpelVariance16x16VerticalNEON(
		unsafe.SliceData(src),
		srcStride,
		unsafe.SliceData(ref),
		refStride,
		uint64(uint16(filter[0])),
		uint64(uint16(filter[1])),
		&sum,
		&sse,
	)
	return int(sum), int(sse), true
}

func subpelVariance16x16Bilinear(src []byte, srcStride int, xOffset int, yOffset int, ref []byte, refStride int) (int, int, bool) {
	xFilter := tables.BilinearFilters[xOffset]
	yFilter := tables.BilinearFilters[yOffset]
	if !dspSIMDPredictWindowOK(src, srcStride, 32, 17, ref, refStride, 16, 16) {
		return 0, 0, false
	}
	var sum int32
	var sse uint32
	subpelVariance16x16BilinearNEON(
		unsafe.SliceData(src),
		srcStride,
		unsafe.SliceData(ref),
		refStride,
		uint64(uint16(xFilter[0])),
		uint64(uint16(xFilter[1])),
		uint64(uint16(yFilter[0])),
		uint64(uint16(yFilter[1])),
		&sum,
		&sse,
	)
	return int(sum), int(sse), true
}

func subpelVariance16x16PtrFast(src *byte, srcStride int, xOffset int, yOffset int, ref *byte, refStride int) (int, int) {
	if xOffset == 0 && yOffset == 0 {
		sum, sse := VarianceBlock16x16PtrFast(src, srcStride, ref, refStride)
		return sse - (sum * sum >> 8), sse
	}
	var sum int32
	var sse uint32
	if xOffset == 0 {
		filter := tables.BilinearFilters[yOffset]
		subpelVariance16x16VerticalNEON(
			src,
			srcStride,
			ref,
			refStride,
			uint64(uint16(filter[0])),
			uint64(uint16(filter[1])),
			&sum,
			&sse,
		)
		return int(sse) - (int(sum) * int(sum) >> 8), int(sse)
	}
	if yOffset == 0 {
		filter := tables.BilinearFilters[xOffset]
		subpelVariance16x16HorizontalNEON(
			src,
			srcStride,
			ref,
			refStride,
			uint64(uint16(filter[0])),
			uint64(uint16(filter[1])),
			&sum,
			&sse,
		)
		return int(sse) - (int(sum) * int(sum) >> 8), int(sse)
	}
	xFilter := tables.BilinearFilters[xOffset]
	yFilter := tables.BilinearFilters[yOffset]
	subpelVariance16x16BilinearNEON(
		src,
		srcStride,
		ref,
		refStride,
		uint64(uint16(xFilter[0])),
		uint64(uint16(xFilter[1])),
		uint64(uint16(yFilter[0])),
		uint64(uint16(yFilter[1])),
		&sum,
		&sse,
	)
	return int(sse) - (int(sum) * int(sum) >> 8), int(sse)
}

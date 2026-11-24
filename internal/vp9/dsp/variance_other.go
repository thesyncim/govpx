package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Size-specialized variance helpers share Go SIMD and scalar kernels.

func varianceStatsStandard(w, h int, src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride int,
) VarianceStats {
	sum, sse := pixel.Stats(src[srcOff:], srcStride, ref[refOff:], refStride, w, h)
	return varianceStatsFromSumSSE(int32(sum), sse, w, h)
}

func variance64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(64, 64, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(64, 32, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(32, 64, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(32, 32, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(32, 16, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(16, 32, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(16, 16, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(16, 8, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(8, 16, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(8, 8, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(8, 4, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(4, 8, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

func variance4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	stats := varianceStatsStandard(4, 4, src, srcOff, srcStride, ref, refOff, refStride)
	*sse = stats.SSE
	return stats.Variance
}

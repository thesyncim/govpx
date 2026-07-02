//go:build arm64 && !purego

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/cpu"
)

// VP9 ARMv8 NEON variance kernels. Mirrors libvpx v1.16.0
// vpx_dsp/arm/variance_neon.c variance_neon_w16/w8/w4 plus a chunked
// helper for 32 and 64-wide blocks.
//
// Each helper writes the raw sum (int32) and sse (uint32) of
// (src - ref) over the (w, h) block; the public VpxVariance{W}x{H}
// wrappers compute the final variance value from those two scalars.
//
// Sum lane accumulation uses int16. With max h=64 and w<=64, the
// per-lane bound is 64*255 = 16320 which fits in signed int16, so we
// horizontally reduce via SADDLV only at the end.

//go:noescape
func varianceBlock16xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock16ChunksNEON(src *byte, srcStride int, ref *byte, refStride int, height int, chunks int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock8xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock4xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceDotChunksNEON(src *byte, srcStride int, ref *byte, refStride int, height int, chunks int, sumOut *int32, sseOut *uint32)

// variance16xNKernel dispatches the 16/32/64-wide variance kernel:
// the FEAT_DotProd port of variance_16xh/variance_large_neon_dotprod
// when available, else the base NEON kernels.
func variance16xNKernel(src *byte, srcStride int, ref *byte, refStride int,
	w, h int, sum *int32, sse *uint32,
) bool {
	if w != 16 && w != 32 && w != 64 {
		return false
	}
	if cpu.HasARM64DotProd {
		varianceDotChunksNEON(src, srcStride, ref, refStride, h, w/16, sum, sse)
		return true
	}
	switch w {
	case 16:
		varianceBlock16xNNEON(src, srcStride, ref, refStride, h, sum, sse)
	case 32:
		varianceBlock16ChunksNEON(src, srcStride, ref, refStride, h, 2, sum, sse)
	case 64:
		varianceBlock16ChunksNEON(src, srcStride, ref, refStride, h, 4, sum, sse)
	}
	return true
}

// varWindowOK validates the (w, h) read window lies inside buf. Same
// invariants as the SAD wrappers.
func varWindowOK(buf []uint8, off, stride, w, h int) bool {
	if off < 0 || stride < 0 {
		return false
	}
	limit := off + (h-1)*stride + w
	return limit >= off && limit <= len(buf)
}

// finalVariance computes the libvpx variance formula:
//
//	var = sse - sum*sum / (w*h)
func finalVariance(sum int32, sse uint32, w, h int) uint32 {
	return sse - uint32((int64(sum)*int64(sum))/int64(w*h))
}

func varianceStatsStandard(w, h int, src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride int,
) VarianceStats {
	switch {
	case w == 16 || w == 32 || w == 64:
		if stats, ok := varianceStatsSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, w, h); ok {
			return stats
		}
	case w == 8:
		if stats, ok := varianceStatsSimd8xN(src, srcOff, srcStride, ref, refOff, refStride, h); ok {
			return stats
		}
	case w == 4:
		if stats, ok := varianceStatsSimd4xN(src, srcOff, srcStride, ref, refOff, refStride, h); ok {
			return stats
		}
	}
	return varianceStatsScalar(w, h, src, srcOff, srcStride, ref, refOff, refStride)
}

func varianceStatsSimd16xN(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride, w, h int) (VarianceStats, bool) {
	if !varWindowOK(src, srcOff, srcStride, w, h) || !varWindowOK(ref, refOff, refStride, w, h) {
		return VarianceStats{}, false
	}
	var sum int32
	var s uint32
	if !variance16xNKernel(unsafe.SliceData(src[srcOff:]), srcStride,
		unsafe.SliceData(ref[refOff:]), refStride, w, h, &sum, &s) {
		return VarianceStats{}, false
	}
	return varianceStatsFromSumSSE(sum, s, w, h), true
}

func varianceStatsSimd8xN(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride, h int) (VarianceStats, bool) {
	if !varWindowOK(src, srcOff, srcStride, 8, h) || !varWindowOK(ref, refOff, refStride, 8, h) {
		return VarianceStats{}, false
	}
	var sum int32
	var s uint32
	varianceBlock8xNNEON(
		unsafe.SliceData(src[srcOff:]), srcStride,
		unsafe.SliceData(ref[refOff:]), refStride,
		h, &sum, &s)
	return varianceStatsFromSumSSE(sum, s, 8, h), true
}

func varianceStatsSimd4xN(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride, h int) (VarianceStats, bool) {
	if h&1 != 0 {
		return VarianceStats{}, false
	}
	if !varWindowOK(src, srcOff, srcStride, 4, h) || !varWindowOK(ref, refOff, refStride, 4, h) {
		return VarianceStats{}, false
	}
	var sum int32
	var s uint32
	varianceBlock4xNNEON(
		unsafe.SliceData(src[srcOff:]), srcStride,
		unsafe.SliceData(ref[refOff:]), refStride,
		h, &sum, &s)
	return varianceStatsFromSumSSE(sum, s, 4, h), true
}

func varianceSimd16xN(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride, w, h int, sse *uint32) (uint32, bool) {
	if !varWindowOK(src, srcOff, srcStride, w, h) || !varWindowOK(ref, refOff, refStride, w, h) {
		return 0, false
	}
	var sum int32
	var s uint32
	if !variance16xNKernel(unsafe.SliceData(src[srcOff:]), srcStride,
		unsafe.SliceData(ref[refOff:]), refStride, w, h, &sum, &s) {
		return 0, false
	}
	*sse = s
	return finalVariance(sum, s, w, h), true
}

func varianceSimd8xN(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride, h int, sse *uint32) (uint32, bool) {
	if !varWindowOK(src, srcOff, srcStride, 8, h) || !varWindowOK(ref, refOff, refStride, 8, h) {
		return 0, false
	}
	var sum int32
	var s uint32
	varianceBlock8xNNEON(
		unsafe.SliceData(src[srcOff:]), srcStride,
		unsafe.SliceData(ref[refOff:]), refStride,
		h, &sum, &s)
	*sse = s
	return finalVariance(sum, s, 8, h), true
}

func varianceSimd4xN(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride, h int, sse *uint32) (uint32, bool) {
	if h&1 != 0 {
		return 0, false
	}
	if !varWindowOK(src, srcOff, srcStride, 4, h) || !varWindowOK(ref, refOff, refStride, 4, h) {
		return 0, false
	}
	var sum int32
	var s uint32
	varianceBlock4xNNEON(
		unsafe.SliceData(src[srcOff:]), srcStride,
		unsafe.SliceData(ref[refOff:]), refStride,
		h, &sum, &s)
	*sse = s
	return finalVariance(sum, s, 4, h), true
}

// Size-specialized variance helpers. Each tries the NEON SIMD path
// first and falls back to the scalar reference on a window mismatch.

func variance64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 64, 64, sse); ok {
		return v
	}
	return varianceScalar(64, 64, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 64, 32, sse); ok {
		return v
	}
	return varianceScalar(64, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 32, 64, sse); ok {
		return v
	}
	return varianceScalar(32, 64, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 32, 32, sse); ok {
		return v
	}
	return varianceScalar(32, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 32, 16, sse); ok {
		return v
	}
	return varianceScalar(32, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 16, 32, sse); ok {
		return v
	}
	return varianceScalar(16, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 16, 16, sse); ok {
		return v
	}
	return varianceScalar(16, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd16xN(src, srcOff, srcStride, ref, refOff, refStride, 16, 8, sse); ok {
		return v
	}
	return varianceScalar(16, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd8xN(src, srcOff, srcStride, ref, refOff, refStride, 16, sse); ok {
		return v
	}
	return varianceScalar(8, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd8xN(src, srcOff, srcStride, ref, refOff, refStride, 8, sse); ok {
		return v
	}
	return varianceScalar(8, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd8xN(src, srcOff, srcStride, ref, refOff, refStride, 4, sse); ok {
		return v
	}
	return varianceScalar(8, 4, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd4xN(src, srcOff, srcStride, ref, refOff, refStride, 8, sse); ok {
		return v
	}
	return varianceScalar(4, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

func variance4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	if v, ok := varianceSimd4xN(src, srcOff, srcStride, ref, refOff, refStride, 4, sse); ok {
		return v
	}
	return varianceScalar(4, 4, src, srcOff, srcStride, ref, refOff, refStride, sse)
}

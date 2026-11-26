package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// VP9 variance kernels. Ported from libvpx v1.16.0 vpx_dsp/variance.c.
// variance(W, H) returns sse - (sum*sum) / (W*H) along with the raw
// sse via a pointer; MSE returns just the sse. The subpel variance
// variants (which require the 2-tap bilinear pre-filter) land
// separately once the encoder needs them.

// computeVariance is the parametric helper; mirrors the static
// `variance` in vpx_dsp/variance.c.
func computeVariance(src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride, w, h int,
) (sse uint32, sum int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	sum, sse = pixel.Stats(src[srcOff:], srcStride, ref[refOff:], refStride, w, h)
	return sse, sum
}

func varianceScalar(w, h int, src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	s, sum := computeVariance(src, srcOff, srcStride, ref, refOff, refStride, w, h)
	*sse = s
	return s - uint32((int64(sum)*int64(sum))/int64(w*h))
}

// VarianceStats is the raw libvpx variance accumulator state for a block.
type VarianceStats struct {
	Variance uint32
	SSE      uint32
	Sum      int32
}

func varianceStatsFromSumSSE(sum int32, sse uint32, w, h int) VarianceStats {
	return VarianceStats{
		Variance: sse - uint32((int64(sum)*int64(sum))/int64(w*h)),
		SSE:      sse,
		Sum:      sum,
	}
}

// VpxVarianceStats returns variance, SSE, and signed sum for the standard VP9
// variance block sizes. Unsupported sizes return ok=false.
func VpxVarianceStats(src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride int, w, h int,
) (VarianceStats, bool) {
	switch {
	case (w == 64 && (h == 64 || h == 32)) ||
		(w == 32 && (h == 64 || h == 32 || h == 16)) ||
		(w == 16 && (h == 32 || h == 16 || h == 8)) ||
		(w == 8 && (h == 16 || h == 8 || h == 4)) ||
		(w == 4 && (h == 8 || h == 4)):
		return varianceStatsStandard(w, h, src, srcOff, srcStride, ref, refOff, refStride), true
	default:
		return VarianceStats{}, false
	}
}

// VpxVariance{W}x{H} mirror libvpx's vpx_variance{W}x{H}_c. Each
// delegates to a size-specialized internal helper so per-arch SIMD
// backends can override the hot sizes (16+) while the small sizes
// stay on the scalar reference.

func VpxVariance64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance64x64(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance64x32(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance32x64(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance32x32(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance32x16(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance16x32(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance16x16(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance16x8(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance8x16(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance8x8(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance8x4(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance4x8(src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance4x4(src, srcOff, srcStride, ref, refOff, refStride, sse)
}

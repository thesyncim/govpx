package dsp

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
	for y := range h {
		srcRow := srcOff + y*srcStride
		refRow := refOff + y*refStride
		for x := range w {
			diff := int(src[srcRow+x]) - int(ref[refRow+x])
			sum += diff
			sse += uint32(diff * diff)
		}
	}
	return
}

func varianceScalar(w, h int, src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	s, sum := computeVariance(src, srcOff, srcStride, ref, refOff, refStride, w, h)
	*sse = s
	return s - uint32((int64(sum)*int64(sum))/int64(w*h))
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

// VpxMse{W}x{H} mirror vpx_mse{W}x{H}_c — return just the sse.
func VpxMse16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	s, _ := computeVariance(src, srcOff, srcStride, ref, refOff, refStride, 16, 16)
	*sse = s
	return s
}
func VpxMse16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	s, _ := computeVariance(src, srcOff, srcStride, ref, refOff, refStride, 16, 8)
	*sse = s
	return s
}
func VpxMse8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	s, _ := computeVariance(src, srcOff, srcStride, ref, refOff, refStride, 8, 16)
	*sse = s
	return s
}
func VpxMse8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	s, _ := computeVariance(src, srcOff, srcStride, ref, refOff, refStride, 8, 8)
	*sse = s
	return s
}

// VpxGet4x4SseCs is the simple 4x4 SSE helper used by the encoder's
// CS-mode search; mirrors vpx_get4x4sse_cs_c.
func VpxGet4x4SseCs(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int) uint32 {
	var d uint32
	for y := range 4 {
		srcRow := srcOff + y*srcStride
		refRow := refOff + y*refStride
		for x := range 4 {
			diff := int(src[srcRow+x]) - int(ref[refRow+x])
			d += uint32(diff * diff)
		}
	}
	return d
}

// VpxGetMbSs mirrors vpx_get_mb_ss_c. The input is a 256-element int16
// residual buffer; the helper just returns the sum-of-squares.
func VpxGetMbSs(src []int16) uint32 {
	var sum uint32
	for i := range 256 {
		sum += uint32(int32(src[i]) * int32(src[i]))
	}
	return sum
}

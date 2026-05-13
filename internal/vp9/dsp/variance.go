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
	for y := 0; y < h; y++ {
		srcRow := srcOff + y*srcStride
		refRow := refOff + y*refStride
		for x := 0; x < w; x++ {
			diff := int(src[srcRow+x]) - int(ref[refRow+x])
			sum += diff
			sse += uint32(diff * diff)
		}
	}
	return
}

func variance(w, h int, src []uint8, srcOff, srcStride int,
	ref []uint8, refOff, refStride int, sse *uint32,
) uint32 {
	s, sum := computeVariance(src, srcOff, srcStride, ref, refOff, refStride, w, h)
	*sse = s
	return s - uint32((int64(sum)*int64(sum))/int64(w*h))
}

// VpxVariance{W}x{H} mirror libvpx's vpx_variance{W}x{H}_c.

func VpxVariance64x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(64, 64, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance64x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(64, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance32x64(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(32, 64, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance32x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(32, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance32x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(32, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance16x32(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(16, 32, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance16x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(16, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance16x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(16, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance8x16(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(8, 16, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance8x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(8, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance8x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(8, 4, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance4x8(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(4, 8, src, srcOff, srcStride, ref, refOff, refStride, sse)
}
func VpxVariance4x4(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride int, sse *uint32) uint32 {
	return variance(4, 4, src, srcOff, srcStride, ref, refOff, refStride, sse)
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
	for y := 0; y < 4; y++ {
		srcRow := srcOff + y*srcStride
		refRow := refOff + y*refStride
		for x := 0; x < 4; x++ {
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
	for i := 0; i < 256; i++ {
		sum += uint32(int32(src[i]) * int32(src[i]))
	}
	return sum
}

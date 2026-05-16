//go:build amd64 && !purego

package dsp

import "unsafe"

// VP9 AMD64 SSE2 variance kernels. Mirrors libvpx v1.16.0
// vpx_dsp/x86/variance_sse2.c variance_sse2.h variance_kernel helpers.
//
// Each helper writes the raw sum (int32) and sse (uint32) of
// (src - ref) over the (w, h) block; the public VpxVariance{W}x{H}
// wrappers compute the final variance value from those two scalars.
//
// SSE2 strategy: compute |src - ref| via PSUBUSB(src, ref) | PSUBUSB(ref,
// src) so we can use unsigned arithmetic for the squared sum (PMADDWD
// of int16-widened abs-diffs). The signed sum is recovered as
// PSADBW(src, ref-as-positive) - PSADBW(ref, src-as-positive); we
// accumulate the two PSADBW results separately and subtract once at the
// end. PMADDWD overflow is bounded for 64x64 because each int32 lane
// accumulates pairs of (255*255) = 65025 → over a 64x64 block per lane
// (2 lanes per 8 input bytes), we have ≤ 8 * 64 = 512 pairs per lane
// per chunk; 512 * 65025 = 33,292,800, well below INT32_MAX. PSADBW
// accumulates uint16 sums per 8-byte half-lane bounded by 8*255 = 2040
// per half-lane; over 64*64 we accumulate ≤ 64*4 = 256 chunks of half-
// lane data, max 256*2040 = 522,240, well within the int32 lane.

//go:noescape
func varianceBlock16xNSSE2(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock16ChunksSSE2(src *byte, srcStride int, ref *byte, refStride int, height int, chunks int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock8xNSSE2(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock4xNSSE2(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

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

func varianceSimd16xN(src []uint8, srcOff, srcStride int, ref []uint8, refOff, refStride, w, h int, sse *uint32) (uint32, bool) {
	if !varWindowOK(src, srcOff, srcStride, w, h) || !varWindowOK(ref, refOff, refStride, w, h) {
		return 0, false
	}
	var sum int32
	var s uint32
	switch w {
	case 16:
		varianceBlock16xNSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride,
			h, &sum, &s)
	case 32:
		varianceBlock16ChunksSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride,
			h, 2, &sum, &s)
	case 64:
		varianceBlock16ChunksSSE2(
			unsafe.SliceData(src[srcOff:]), srcStride,
			unsafe.SliceData(ref[refOff:]), refStride,
			h, 4, &sum, &s)
	default:
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
	varianceBlock8xNSSE2(
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
	varianceBlock4xNSSE2(
		unsafe.SliceData(src[srcOff:]), srcStride,
		unsafe.SliceData(ref[refOff:]), refStride,
		h, &sum, &s)
	*sse = s
	return finalVariance(sum, s, 4, h), true
}

// Size-specialized variance helpers. Each tries the SSE2 SIMD path
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

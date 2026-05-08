//go:build amd64

package dsp

// amd64 SSE2 dispatch for VP8 intra-prediction primitives. Mirrors the
// libvpx v1.16.0 vp8/common/x86/vp8_intrapred_sse2.asm per-mode kernels
// and vp8/common/reconintra.c availability semantics. SSE2 is part of
// the x86-64 baseline so the SIMD entry points are always safe to call
// without runtime detection.

func intraDCPredict16x16(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	_ = dst[15*dstStride+15]
	dc := byte(128)
	switch {
	case upAvailable && leftAvailable:
		_ = above[15]
		_ = left[15]
		sum := int(intraSum16SSE2(&above[0])) + int(intraSum16SSE2(&left[0]))
		dc = byte((sum + 16) / 32)
	case upAvailable:
		_ = above[15]
		sum := int(intraSum16SSE2(&above[0]))
		dc = byte((sum + 8) / 16)
	case leftAvailable:
		_ = left[15]
		sum := int(intraSum16SSE2(&left[0]))
		dc = byte((sum + 8) / 16)
	}
	intraFill16x16SSE2(&dst[0], dstStride, dc)
}

func intraDCPredict8x8(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	_ = dst[7*dstStride+7]
	dc := byte(128)
	switch {
	case upAvailable && leftAvailable:
		_ = above[7]
		_ = left[7]
		sum := int(intraSum8SSE2(&above[0])) + int(intraSum8SSE2(&left[0]))
		dc = byte((sum + 8) / 16)
	case upAvailable:
		_ = above[7]
		sum := int(intraSum8SSE2(&above[0]))
		dc = byte((sum + 4) / 8)
	case leftAvailable:
		_ = left[7]
		sum := int(intraSum8SSE2(&left[0]))
		dc = byte((sum + 4) / 8)
	}
	intraFill8x8SSE2(&dst[0], dstStride, dc)
}

func intraVerticalPredict16x16(dst []byte, dstStride int, above []byte) {
	// Pure-Go copy is already memcpy-optimal; the SIMD form would only
	// add dispatch overhead for a 16-byte per-row store.
	intraVerticalPredictScalar(dst, dstStride, above, 16)
}

func intraVerticalPredict8x8(dst []byte, dstStride int, above []byte) {
	intraVerticalPredictScalar(dst, dstStride, above, 8)
}

func intraHorizontalPredict16x16(dst []byte, dstStride int, left []byte) {
	_ = left[15]
	_ = dst[15*dstStride+15]
	intraHPredict16x16SSE2(&dst[0], dstStride, &left[0])
}

func intraHorizontalPredict8x8(dst []byte, dstStride int, left []byte) {
	_ = left[7]
	_ = dst[7*dstStride+7]
	intraHPredict8x8SSE2(&dst[0], dstStride, &left[0])
}

func intraTMPredict16x16(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	_ = above[15]
	_ = left[15]
	_ = dst[15*dstStride+15]
	intraTMPredict16x16SSE2(&dst[0], dstStride, &above[0], &left[0], topLeft)
}

func intraTMPredict8x8(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	_ = above[7]
	_ = left[7]
	_ = dst[7*dstStride+7]
	intraTMPredict8x8SSE2(&dst[0], dstStride, &above[0], &left[0], topLeft)
}

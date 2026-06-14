//go:build arm64 && !purego

package dsp

import "unsafe"

// arm64 NEON dispatch for VP8 intra-prediction primitives. Mirrors the
// libvpx v1.16.0 vp8/common/arm/neon/vp8_intrapred_neon.c per-mode
// kernels and vp8/common/reconintra.c availability semantics.
//
// Each wrapper validates the full block/edge window before calling into
// the NEON kernel; once proven in-bounds we fetch the base via
// unsafe.SliceData to skip the secondary bounds-check + stack frame the
// compiler would otherwise emit for &slice[0].

func intraDCPredict16x16(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	if !intraDCPredictWindowOK(dst, dstStride, above, left, 16, upAvailable, leftAvailable) {
		intraDCPredictScalar(dst, dstStride, above, left, 16, upAvailable, leftAvailable)
		return
	}
	dc := byte(128)
	switch {
	case upAvailable && leftAvailable:
		sum := int(intraSum16NEON(unsafe.SliceData(above))) + int(intraSum16NEON(unsafe.SliceData(left)))
		dc = byte((sum + 16) / 32)
	case upAvailable:
		sum := int(intraSum16NEON(unsafe.SliceData(above)))
		dc = byte((sum + 8) / 16)
	case leftAvailable:
		sum := int(intraSum16NEON(unsafe.SliceData(left)))
		dc = byte((sum + 8) / 16)
	}
	intraFill16x16NEON(unsafe.SliceData(dst), dstStride, dc)
}

func intraDCPredict8x8(dst []byte, dstStride int, above []byte, left []byte, upAvailable bool, leftAvailable bool) {
	if !intraDCPredictWindowOK(dst, dstStride, above, left, 8, upAvailable, leftAvailable) {
		intraDCPredictScalar(dst, dstStride, above, left, 8, upAvailable, leftAvailable)
		return
	}
	dc := byte(128)
	switch {
	case upAvailable && leftAvailable:
		sum := int(intraSum8NEON(unsafe.SliceData(above))) + int(intraSum8NEON(unsafe.SliceData(left)))
		dc = byte((sum + 8) / 16)
	case upAvailable:
		sum := int(intraSum8NEON(unsafe.SliceData(above)))
		dc = byte((sum + 4) / 8)
	case leftAvailable:
		sum := int(intraSum8NEON(unsafe.SliceData(left)))
		dc = byte((sum + 4) / 8)
	}
	intraFill8x8NEON(unsafe.SliceData(dst), dstStride, dc)
}

func intraVerticalPredict16x16(dst []byte, dstStride int, above []byte) {
	// Pure-Go copy compiles to a single MOVOU per row; the NEON kernel
	// is functionally equivalent but adds a call/return cycle that hurts
	// at this scale. Keep the scalar memcopy path. The intraVPredict*NEON
	// kernels are kept available for callers that want to avoid the loop
	// boilerplate.
	intraVerticalPredictScalar(dst, dstStride, above, 16)
}

func intraVerticalPredict8x8(dst []byte, dstStride int, above []byte) {
	intraVerticalPredictScalar(dst, dstStride, above, 8)
}

func intraHorizontalPredict16x16(dst []byte, dstStride int, left []byte) {
	if !intraPredictWindowOK(dst, dstStride, left, 16) {
		intraHorizontalPredictScalar(dst, dstStride, left, 16)
		return
	}
	intraHPredict16x16NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(left))
}

func intraHorizontalPredict8x8(dst []byte, dstStride int, left []byte) {
	if !intraPredictWindowOK(dst, dstStride, left, 8) {
		intraHorizontalPredictScalar(dst, dstStride, left, 8)
		return
	}
	intraHPredict8x8NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(left))
}

func intraTMPredict16x16(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intraTMPredictWindowOK(dst, dstStride, above, left, 16) {
		intraTMPredictScalar(dst, dstStride, above, left, topLeft, 16)
		return
	}
	intraTMPredict16x16NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

func intraTMPredict8x8(dst []byte, dstStride int, above []byte, left []byte, topLeft byte) {
	if !intraTMPredictWindowOK(dst, dstStride, above, left, 8) {
		intraTMPredictScalar(dst, dstStride, above, left, topLeft, 8)
		return
	}
	intraTMPredict8x8NEON(unsafe.SliceData(dst), dstStride, unsafe.SliceData(above), unsafe.SliceData(left), topLeft)
}

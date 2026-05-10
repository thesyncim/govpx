//go:build !arm64 && !amd64

package dsp

import "unsafe"

// Pure-Go fallback for the non-16x16 variance block kernels on
// architectures without a SIMD port. Mirrors libvpx v1.16.0
// vpx_dsp/variance.c semantics by deferring to varianceBlockGeneric.

func varianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, width, height int) (int, int) {
	return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
}

// VarianceBlock8x8PtrFast is the pointer-form fallback used when callers
// have already validated the 8x8 window is in-bounds.
func VarianceBlock8x8PtrFast(src *byte, srcStride int, ref *byte, refStride int) (int, int) {
	return varianceBlockGeneric(unsafe.Slice(src, 8*srcStride), srcStride, unsafe.Slice(ref, 8*refStride), refStride, 8, 8)
}

func sse8x8PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	_, sse := VarianceBlock8x8PtrFast(src, srcStride, ref, refStride)
	return sse
}

func SSE16xNPtrFast(src *byte, srcStride int, ref *byte, refStride int, height int) int {
	_, sse := varianceBlockGeneric(unsafe.Slice(src, height*srcStride), srcStride, unsafe.Slice(ref, height*refStride), refStride, 16, height)
	return sse
}

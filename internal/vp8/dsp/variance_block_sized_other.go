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

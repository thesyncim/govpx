//go:build !arm64 && !amd64

package dsp

import "unsafe"

// Pure-Go fallback for the 16x16 variance block on architectures
// without a SIMD port. Mirrors libvpx v1.16.0 vpx_dsp/variance.c
// semantics by deferring to varianceBlockGeneric.

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	return varianceBlockGeneric(src, srcStride, ref, refStride, 16, 16)
}

// VarianceBlock16x16PtrFast is the pointer-form fallback used by hot
// callers when bounds have already been validated. The fallback build
// has no SIMD kernel to bypass so this just forwards to the slice form,
// matching the byte-identity contract.
func VarianceBlock16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) (int, int) {
	return varianceBlockGeneric(unsafePtrToSlice(src, 16*srcStride), srcStride, unsafePtrToSlice(ref, 16*refStride), refStride, 16, 16)
}

func sse16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	_, sse := VarianceBlock16x16PtrFast(src, srcStride, ref, refStride)
	return sse
}

func unsafePtrToSlice(p *byte, n int) []byte {
	return unsafe.Slice(p, n)
}

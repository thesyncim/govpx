//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback for the 16x16 variance block on architectures
// without a SIMD port. Mirrors libvpx v1.16.0 vpx_dsp/variance.c
// semantics by deferring to varianceBlockGeneric.

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	return varianceBlockGeneric(src, srcStride, ref, refStride, 16, 16)
}

//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback for the non-16x16 variance block kernels on
// architectures without a SIMD port. Mirrors libvpx v1.16.0
// vpx_dsp/variance.c semantics by deferring to varianceBlockGeneric.

func varianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, width, height int) (int, int) {
	return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
}

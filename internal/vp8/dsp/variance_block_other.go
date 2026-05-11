//go:build (!arm64 && !amd64) || purego

package dsp

// Pure-Go fallback for the 16x16 variance block on architectures
// without a SIMD port. Mirrors libvpx v1.16.0 vpx_dsp/variance.c
// semantics with a width-specialized scalar loop.

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	return varianceBlock16xNScalar(src, srcStride, ref, refStride, 16)
}

// VarianceBlock16x16PtrFast is the pointer-form fallback used by hot
// callers when bounds have already been validated. The fallback build
// has no SIMD kernel to bypass, so this uses the same width-specialized
// scalar loop while keeping the pointer-form hot-call contract.
func VarianceBlock16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) (int, int) {
	return varianceBlock16xNPtrScalar(src, srcStride, ref, refStride, 16)
}

func sse16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	_, sse := VarianceBlock16x16PtrFast(src, srcStride, ref, refStride)
	return sse
}

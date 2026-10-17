//go:build !goexperiment.simd || !arm64 || purego

package dsp

// Limited SAD follows libvpx v1.16.0 vp8/encoder/mcomp.c row boundaries.

// SAD16x16LimitPtrFast returns after the first row exceeding limit.
// Both pointers must address readable 16x16 blocks.
func SAD16x16LimitPtrFast(src *byte, srcStride int, ref *byte, refStride int, limit int) int {
	return sadBlockLimitScalarPtr(src, srcStride, ref, refStride, 16, 16, limit)
}

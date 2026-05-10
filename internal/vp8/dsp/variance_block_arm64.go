//go:build arm64

package dsp

import "unsafe"

// ARMv8 NEON port of the libvpx v1.16.0 vpx_dsp/arm/variance_neon.c
// 16x16 variance block. Computes (sum, sse) where:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// for a 16x16 block. NEON does USUBL on byte pairs to get int16
// diffs, SADALP to pairwise-accumulate diffs into int32 sum lanes,
// and SMLAL/SMLAL2 to square+accumulate into int32 sse lanes. After
// the row loop, VADDV reduces both accumulators to scalars.

//go:noescape
func varianceBlock16x16NEON(src *byte, srcStride int, ref *byte, refStride int, sumOut *int32, sseOut *uint32)

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	var sum int32
	var sse uint32
	// unsafe.SliceData skips the runtime.panicBounds + stack frame the
	// compiler emits for &src[0] / &ref[0]. Hot motion-search callers
	// pass non-empty slices shaped to cover the 16x16 read window.
	varianceBlock16x16NEON(unsafe.SliceData(src), srcStride, unsafe.SliceData(ref), refStride, &sum, &sse)
	return int(sum), int(sse)
}

// VarianceBlock16x16PtrFast is the SIMD-bypass entry point used by hot
// callers (loop-filter SSE trial, mode-picker SSE/variance walks). The
// caller must have already validated that src and ref point to 16x16
// windows fully in-bounds.
func VarianceBlock16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) (int, int) {
	var sum int32
	var sse uint32
	varianceBlock16x16NEON(src, srcStride, ref, refStride, &sum, &sse)
	return int(sum), int(sse)
}

func sse16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	var sse uint32
	sseBlock16xNNEON(src, srcStride, ref, refStride, 16, &sse)
	return int(sse)
}

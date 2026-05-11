//go:build arm64

package dsp

import "unsafe"

// ARMv8 NEON ports of the libvpx v1.16.0 vpx_dsp/arm/variance_neon.c
// variance kernels for the smaller (non-16x16) block sizes used by the
// VP8 inter-mode picker:
//
//   variance16x8   (16-wide, height-parameterised)
//   variance8x{16,8,4} (8-wide, height-parameterised)
//   variance4x{8,4}    (4-wide, height-parameterised)
//
// Each kernel computes:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// The 16x16 path keeps its own kernel in variance_block_arm64.s — these
// helpers cover everything else.

//go:noescape
func varianceBlock16xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock8xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock4xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func sseBlock16xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sseOut *uint32)

//go:noescape
func sseBlock8xNNEON(src *byte, srcStride int, ref *byte, refStride int, height int, sseOut *uint32)

// varianceBlockSized fans out per-width to the matching NEON kernel.
// Slice bases go via unsafe.SliceData so the dispatch stays free of
// runtime.panicBounds and a stack frame for &src[0] / &ref[0]; callers
// always pass non-empty slices shaped to cover the read window.
func varianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, width, height int) (int, int) {
	var sum int32
	var sse uint32
	srcPtr := unsafe.SliceData(src)
	refPtr := unsafe.SliceData(ref)
	switch width {
	case 16:
		varianceBlock16xNNEON(srcPtr, srcStride, refPtr, refStride, height, &sum, &sse)
	case 8:
		varianceBlock8xNNEON(srcPtr, srcStride, refPtr, refStride, height, &sum, &sse)
	case 4:
		varianceBlock4xNNEON(srcPtr, srcStride, refPtr, refStride, height, &sum, &sse)
	default:
		return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
	}
	return int(sum), int(sse)
}

func sse8x8PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	var sse uint32
	sseBlock8xNNEON(src, srcStride, ref, refStride, 8, &sse)
	return int(sse)
}

func SSE16xNPtrFast(src *byte, srcStride int, ref *byte, refStride int, height int) int {
	var sse uint32
	sseBlock16xNNEON(src, srcStride, ref, refStride, height, &sse)
	return int(sse)
}

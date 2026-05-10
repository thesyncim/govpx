//go:build amd64

package dsp

import (
	"unsafe"

	"github.com/thesyncim/govpx/internal/cpu"
)

// SSE2 port of the libvpx v1.16.0 vpx_dsp/x86/variance_sse2.c 16x16
// variance block. Computes (sum, sse) where:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// for a 16x16 block. SSE2 unpacks bytes to int16 lanes, PSUBW to get
// 16-bit diffs, PMADDWD squares + accumulates into int32 sse lanes,
// PADDW accumulates into a 16-bit sum register that's sign-extended
// at the end. On AVX2-capable CPUs we route through
// varianceBlock16xNAVX2 with height=16 for ~2x throughput.

//go:noescape
func varianceBlock16x16SSE2(src *byte, srcStride int, ref *byte, refStride int, sumOut *int32, sseOut *uint32)

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	var sum int32
	var sse uint32
	srcPtr := unsafe.SliceData(src)
	refPtr := unsafe.SliceData(ref)
	if cpu.HasAVX2 {
		varianceBlock16xNAVX2(srcPtr, srcStride, refPtr, refStride, 16, &sum, &sse)
	} else {
		varianceBlock16x16SSE2(srcPtr, srcStride, refPtr, refStride, &sum, &sse)
	}
	return int(sum), int(sse)
}

// VarianceBlock16x16PtrFast is the SIMD-bypass entry point used by hot
// callers (loop-filter SSE trial, mode-picker SSE/variance walks). The
// caller must have already validated that src and ref point to 16x16
// windows fully in-bounds.
func VarianceBlock16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) (int, int) {
	var sum int32
	var sse uint32
	if cpu.HasAVX2 {
		varianceBlock16xNAVX2(src, srcStride, ref, refStride, 16, &sum, &sse)
	} else {
		varianceBlock16x16SSE2(src, srcStride, ref, refStride, &sum, &sse)
	}
	return int(sum), int(sse)
}

func sse16x16PtrFast(src *byte, srcStride int, ref *byte, refStride int) int {
	_, sse := VarianceBlock16x16PtrFast(src, srcStride, ref, refStride)
	return sse
}

//go:build amd64

package dsp

// SSE2 port of the libvpx v1.16.0 vpx_dsp/x86/variance_sse2.c 16x16
// variance block. Computes (sum, sse) where:
//
//   sum = SUM_{y,x} (src[y][x] - ref[y][x])
//   sse = SUM_{y,x} (src[y][x] - ref[y][x])^2
//
// for a 16x16 block. SSE2 unpacks bytes to int16 lanes, PSUBW to get
// 16-bit diffs, PMADDWD squares + accumulates into int32 sse lanes,
// PADDW accumulates into a 16-bit sum register that's sign-extended
// at the end.

//go:noescape
func varianceBlock16x16SSE2(src *byte, srcStride int, ref *byte, refStride int, sumOut *int32, sseOut *uint32)

func varianceBlock16x16(src []byte, srcStride int, ref []byte, refStride int) (int, int) {
	var sum int32
	var sse uint32
	varianceBlock16x16SSE2(&src[0], srcStride, &ref[0], refStride, &sum, &sse)
	return int(sum), int(sse)
}

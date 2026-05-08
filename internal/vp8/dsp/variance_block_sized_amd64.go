//go:build amd64

package dsp

// SSE2 ports of the libvpx v1.16.0 vpx_dsp/x86/variance_sse2.c
// variance kernels for the smaller (non-16x16) block sizes used by
// the VP8 inter-mode picker. Each kernel returns (sum, sse) of
// (src - ref) over the whole block and is parameterised by height.
//
// The 16x16 path keeps its own kernel in variance_block_amd64.s — these
// helpers cover everything else.

//go:noescape
func varianceBlock16xNSSE2(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock8xNSSE2(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

//go:noescape
func varianceBlock4xNSSE2(src *byte, srcStride int, ref *byte, refStride int, height int, sumOut *int32, sseOut *uint32)

func varianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, width, height int) (int, int) {
	var sum int32
	var sse uint32
	switch width {
	case 16:
		varianceBlock16xNSSE2(&src[0], srcStride, &ref[0], refStride, height, &sum, &sse)
	case 8:
		varianceBlock8xNSSE2(&src[0], srcStride, &ref[0], refStride, height, &sum, &sse)
	case 4:
		varianceBlock4xNSSE2(&src[0], srcStride, &ref[0], refStride, height, &sum, &sse)
	default:
		return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
	}
	return int(sum), int(sse)
}

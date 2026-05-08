//go:build arm64

package dsp

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

func varianceBlockSized(src []byte, srcStride int, ref []byte, refStride int, width, height int) (int, int) {
	var sum int32
	var sse uint32
	switch width {
	case 16:
		varianceBlock16xNNEON(&src[0], srcStride, &ref[0], refStride, height, &sum, &sse)
	case 8:
		varianceBlock8xNNEON(&src[0], srcStride, &ref[0], refStride, height, &sum, &sse)
	case 4:
		varianceBlock4xNNEON(&src[0], srcStride, &ref[0], refStride, height, &sum, &sse)
	default:
		return varianceBlockGeneric(src, srcStride, ref, refStride, width, height)
	}
	return int(sum), int(sse)
}

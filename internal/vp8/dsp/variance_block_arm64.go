//go:build arm64

package dsp

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
	varianceBlock16x16NEON(&src[0], srcStride, &ref[0], refStride, &sum, &sse)
	return int(sum), int(sse)
}

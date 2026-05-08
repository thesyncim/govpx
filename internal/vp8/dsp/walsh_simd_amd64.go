//go:build amd64

package dsp

// SSE2 port of libvpx v1.16.0 vp8/common/x86/iwalsh_sse2.asm
// (vp8_short_inv_walsh4x4_sse2). Output is byte-identical to
// inverseWalsh4x4Scalar for the decoder's coefficient range.

//go:noescape
func inverseWalsh4x4SSE2(input *int16, mbDQCoeff *int16)

func inverseWalsh4x4SIMD(input *[16]int16, mbDQCoeff []int16) {
	_ = mbDQCoeff[15*16]
	inverseWalsh4x4SSE2(&input[0], &mbDQCoeff[0])
}

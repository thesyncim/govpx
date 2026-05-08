//go:build arm64

package dsp

// NEON port of libvpx v1.16.0 vp8/common/arm/neon/iwalsh_neon.c
// (vp8_short_inv_walsh4x4_neon). Output is byte-identical to
// inverseWalsh4x4Scalar for the decoder's coefficient range.

//go:noescape
func inverseWalsh4x4NEON(input *int16, mbDQCoeff *int16)

func inverseWalsh4x4SIMD(input *[16]int16, mbDQCoeff []int16) {
	_ = mbDQCoeff[15*16]
	inverseWalsh4x4NEON(&input[0], &mbDQCoeff[0])
}

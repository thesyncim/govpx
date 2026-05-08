//go:build arm64

package encoder

// NEON port of libvpx v1.16.0 vp8/encoder/arm/neon/vp8_shortwalsh4x4_neon.c
// (vp8_short_walsh4x4_neon). Output is byte-identical to forwardWalsh4x4Scalar
// for the encoder's residual range.

//go:noescape
func forwardWalsh4x4NEON(input *int16, stride int, output *int16)

func forwardWalsh4x4SIMD(input []int16, stride int, output *[16]int16) {
	forwardWalsh4x4NEON(&input[0], stride, &output[0])
}

//go:build arm64

package encoder

// NEON port of libvpx v1.16.0 vp8/encoder/arm/neon/shortfdct_neon.c
// vp8_short_fdct4x4_neon. Output is byte-identical to ForwardDCT4x4 scalar
// reference for the encoder's input range.

//go:noescape
func forwardDCT4x4NEON(input *int16, stride int, output *int16)

func forwardDCT4x4SIMD(input []int16, stride int, output *[16]int16) {
	forwardDCT4x4NEON(&input[0], stride, &output[0])
}

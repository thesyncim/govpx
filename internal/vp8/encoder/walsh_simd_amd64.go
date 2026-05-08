//go:build amd64

package encoder

// SSE2 port of libvpx v1.16.0 vp8/encoder/x86/fwalsh_sse2.asm
// (vp8_short_walsh4x4_sse2). Output is byte-identical to forwardWalsh4x4Scalar
// for the encoder's residual range.

//go:noescape
func forwardWalsh4x4SSE2(input *int16, stride int, output *int16)

func forwardWalsh4x4SIMD(input []int16, stride int, output *[16]int16) {
	forwardWalsh4x4SSE2(&input[0], stride, &output[0])
}

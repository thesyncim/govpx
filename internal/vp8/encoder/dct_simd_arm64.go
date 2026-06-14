//go:build arm64 && !purego

package encoder

import "unsafe"

// NEON port of libvpx v1.16.0 vp8/encoder/arm/neon/shortfdct_neon.c
// vp8_short_fdct4x4_neon. Output is byte-identical to ForwardDCT4x4 scalar
// reference for the encoder's input range.

//go:noescape
func forwardDCT4x4NEON(input *int16, stride int, output *int16)

func forwardDCT4x4SIMD(input []int16, stride int, output *[16]int16) {
	if !transform4x4WindowOK(input, stride) {
		forwardDCT4x4Scalar(input, stride, output)
		return
	}
	forwardDCT4x4NEON(unsafe.SliceData(input), stride, &output[0])
}

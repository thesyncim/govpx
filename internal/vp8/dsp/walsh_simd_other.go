//go:build !arm64 && !amd64

package dsp

// Pure-Go fallback dispatcher for the InverseWalsh4x4 SIMD entry point.
// Mirrors libvpx v1.16.0 vp8/common/idctllm.c vp8_short_inv_walsh4x4_c
// exactly via inverseWalsh4x4Scalar.

func inverseWalsh4x4SIMD(input *[16]int16, mbDQCoeff []int16) {
	inverseWalsh4x4Scalar(input, mbDQCoeff)
}

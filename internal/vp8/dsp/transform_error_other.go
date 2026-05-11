//go:build (!arm64 && !amd64) || purego

package dsp

// Scalar fallback for the libvpx v1.16.0 vp8_block_error kernel on
// architectures without a SIMD port.

func transformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	return transformBlockErrorScalar(coeff, dqcoeff)
}

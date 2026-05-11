//go:build arm64 && !purego

package dsp

// NEON port of the sum-of-squared-differences kernel from libvpx v1.16.0
// vp8_block_error (vp8/encoder/encodemb.c). The 4x4 block fits in two
// int16x8 vectors, so a single pass of LD1 + SUB + SMULL/SMULL2 +
// horizontal-add suffices.

//go:noescape
func transformBlockErrorNEON(coeff *[16]int16, dqcoeff *[16]int16) int64

func transformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	return int(transformBlockErrorNEON(coeff, dqcoeff))
}

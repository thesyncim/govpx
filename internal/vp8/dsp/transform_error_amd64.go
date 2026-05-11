//go:build amd64 && !purego

package dsp

// SSE2 port of libvpx v1.16.0 vp8_block_error_sse2
// (vp8/encoder/x86/block_error_sse2.asm).

//go:noescape
func transformBlockErrorSSE2(coeff *[16]int16, dqcoeff *[16]int16) int64

func transformBlockError(coeff *[16]int16, dqcoeff *[16]int16) int {
	return int(transformBlockErrorSSE2(coeff, dqcoeff))
}

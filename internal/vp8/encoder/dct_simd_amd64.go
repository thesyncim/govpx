//go:build amd64

package encoder

// ForwardDCT4x4 dispatch on amd64. The libvpx v1.16.0
// vp8/encoder/x86/dct_sse2.asm vp8_short_fdct4x4_sse2 kernel uses a
// non-trivial transpose layout that's tricky to translate into Go's
// amd64 mnemonics with cross-target verification; FastQuantize (4% of
// CPU) carries the heaviest load on amd64, so the FDCT (1.5%) stays
// scalar here pending a follow-up port.

func forwardDCT4x4SIMD(input []int16, stride int, output *[16]int16) {
	forwardDCT4x4Scalar(input, stride, output)
}

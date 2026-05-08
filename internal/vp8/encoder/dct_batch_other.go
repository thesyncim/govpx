//go:build !arm64 && !amd64

package encoder

// Pure-Go fallback dispatcher for the batched 4x4 forward DCT entry
// point. Mirrors libvpx v1.16.0 vp8/encoder/dct.c vp8_short_fdct4x4_c
// applied per block.

func forwardDCT4x4BatchSIMD(input []int16, output []int16, count int) {
	forwardDCT4x4BatchScalar(input, output, count)
}

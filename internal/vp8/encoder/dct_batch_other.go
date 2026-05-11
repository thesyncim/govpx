//go:build (!arm64 && !amd64) || purego

package encoder

// Pure-Go fallback dispatcher for the batched 4x4 forward DCT entry
// point. Mirrors libvpx v1.16.0 vp8/encoder/dct.c vp8_short_fdct4x4_c
// applied per block.

func forwardDCT4x4BatchSIMD(input []int16, output []int16, count int) {
	forwardDCT4x4BatchScalar(input, output, count)
}

func forwardDCT4x4BatchScalar(input []int16, output []int16, count int) {
	for i := range count {
		var out [16]int16
		forwardDCT4x4Scalar(input[i*16:i*16+16], 4, &out)
		copy(output[i*16:i*16+16], out[:])
	}
}

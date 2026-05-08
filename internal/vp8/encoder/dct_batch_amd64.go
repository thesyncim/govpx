//go:build amd64

package encoder

// SSE2 batched port of libvpx v1.16.0 vp8/encoder/x86/dct_sse2.asm
// vp8_short_fdct4x4_sse2. Same per-block kernel as forwardDCT4x4SSE2,
// wrapped in an asm loop so a single Go<->asm transition handles up
// to `count` 4x4 blocks (block stride 4).

//go:noescape
func forwardDCT4x4BatchSSE2(input *int16, output *int16, count int)

func forwardDCT4x4BatchSIMD(input []int16, output []int16, count int) {
	if count <= 0 {
		return
	}
	forwardDCT4x4BatchSSE2(&input[0], &output[0], count)
}

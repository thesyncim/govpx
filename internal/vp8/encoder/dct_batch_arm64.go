//go:build arm64 && !purego

package encoder

// NEON batched port of libvpx v1.16.0
// vp8/encoder/arm/neon/shortfdct_neon.c vp8_short_fdct4x4_neon. The
// per-block kernel is the same byte-identical pipeline as
// forwardDCT4x4NEON; this entry runs the kernel `count` times with
// shared register state and shared constant material to amortize the
// Go<->asm boundary overhead libvpx hides via short_fdct8x4.

//go:noescape
func forwardDCT4x4BatchNEON(input *int16, output *int16, count int)

func forwardDCT4x4BatchSIMD(input []int16, output []int16, count int) {
	if count <= 0 {
		return
	}
	forwardDCT4x4BatchNEON(&input[0], &output[0], count)
}

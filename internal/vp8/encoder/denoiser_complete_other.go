//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package encoder

// Complete luma decisions follow libvpx v1.16.0 vp8/encoder/denoising.c.

func denoiserFilterYCompleteSIMD(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ uint32, _ bool) (int, bool) {
	return 0, false
}

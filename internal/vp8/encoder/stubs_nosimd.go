//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package encoder

// Batched dispatch retains the libvpx v1.16.0 scalar/per-block contract.
func forwardDCT4BatchGo(_ []int16, _ []int16, _ int) int          { return 0 }
func forwardDCT4BatchResidualsGo(_ []int16, _ []int16, _ int) int { return 0 }

// Complete luma decisions follow libvpx v1.16.0 vp8/encoder/denoising.c.
func denoiserFilterYCompleteSIMD(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ uint32, _ bool) (int, bool) {
	return 0, false
}

func denoiserFilterYCompleteAverageSIMD(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ uint32, _ bool) (int, bool) {
	return 0, false
}

// Ported from libvpx v1.16.0.
func quantizeBlockAllZeroSIMD(_ *[16]int16, _ int) bool { return false }

// Arithmetic follows libvpx v1.16.0.
func forward4Go(_ []int16, _ int, _ *[16]int16, _ bool) bool { return false }

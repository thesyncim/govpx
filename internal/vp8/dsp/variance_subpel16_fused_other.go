//go:build !arm64 || purego

package dsp

// Fallback stubs for the fused kernels ported from libvpx v1.16.0
// vpx_dsp/arm/subpel_variance_neon.c on arm64.

func subpelVariance16x16Horizontal(_ []byte, _ int, _ int, _ []byte, _ int) (int, int, bool) {
	return 0, 0, false
}

func subpelVariance16x16Vertical(_ []byte, _ int, _ int, _ []byte, _ int) (int, int, bool) {
	return 0, 0, false
}

func subpelVariance16x16Bilinear(_ []byte, _ int, _ int, _ int, _ []byte, _ int) (int, int, bool) {
	return 0, 0, false
}

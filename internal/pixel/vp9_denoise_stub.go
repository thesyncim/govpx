//go:build !goexperiment.simd || !arm64 || purego

package pixel

// VP9DenoisePlane falls back to the scalar VP9 denoiser on non-arm64/non-SIMD builds.
func VP9DenoisePlane(
	_ []byte, _ int, _ []byte, _ int, _ []byte, _ int,
	_, _, _, _, _, _, _, _, _ int,
) (bool, bool) {
	return false, false
}

//go:build (!arm64 && !amd64) || purego

package encoder

// libvpx v1.16.0 baseline: scalar denoiser fallback when no SIMD build tag matches.

func denoiserFilterYFirstPassSIMD(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ uint32, _ bool) (int, bool) {
	return 0, false
}

func denoiserFilterUVSIMD(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ uint32, _ bool) (int, bool) {
	return 0, false
}

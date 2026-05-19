//go:build (!arm64 && !amd64) || purego

package govpx

func denoiserFilterYFirstPassSIMD(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ uint32, _ bool) (int, bool) {
	return 0, false
}

func denoiserFilterUVSIMD(_ []byte, _ int, _ []byte, _ int, _ []byte, _ int, _ uint32, _ bool) (int, bool) {
	return 0, false
}

//go:build !goexperiment.simd || !arm64 || purego

package pixel

func FilterBilinear2D(_ []byte, _ int, _ []byte, _, _, _ int, _, _, _, _ uint8) bool {
	return false
}

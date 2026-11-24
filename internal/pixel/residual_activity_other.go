//go:build !goexperiment.simd || !arm64 || purego

package pixel

func ResidualSSEActivity(_ []byte, _ int, _ []byte, _ int, _, _ int) (uint64, uint64, bool) {
	return 0, 0, false
}

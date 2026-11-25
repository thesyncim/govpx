//go:build !goexperiment.simd || !arm64 || purego

package pixel

// SumSSE8x8Grid falls back to caller per-block evaluation on non-arm64/non-SIMD builds.
func SumSSE8x8Grid(_ []byte, _ int, _ []byte, _, _, _ int, _ []uint32, _ []int) (uint32, int, bool) {
	return 0, 0, false
}

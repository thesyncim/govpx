//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

// QuantizeVP8Batch leaves unsupported builds to per-block quantization.
func QuantizeVP8Batch(coeff []int16, round, quant, dequant *[16]int16, qcoeff, dqcoeff []int16, iscan *[16]int16, eobs []uint8, count int) bool {
	return false
}

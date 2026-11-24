//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func quantizeVP8SIMD(coeff, round, quant, dequant, qcoeff, dqcoeff, iscan *[16]int16) (int, bool) {
	return 0, false
}

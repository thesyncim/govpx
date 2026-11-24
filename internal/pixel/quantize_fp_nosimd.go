//go:build !goexperiment.simd || (!arm64 && !amd64) || purego

package pixel

func quantizeFPBlockSIMD(coeff, iscan, qcoeff, dqcoeff []int16, n int, r, q, d [2]int16, classes []byte) int {
	return quantizeFPBlockTail(coeff, iscan, qcoeff, dqcoeff, 0, n, r, q, d, classes)
}

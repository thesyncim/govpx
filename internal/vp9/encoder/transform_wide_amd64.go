//go:build goexperiment.simd && amd64 && !purego

package encoder

func forwardDCTWideGo(input []int16, stride int, output []int16, n int) bool {
	switch n {
	case 8:
		return forwardDCT8AVX2(input, stride, output)
	case 16:
		return forwardDCT16AVX2(input, stride, output)
	}
	return false
}

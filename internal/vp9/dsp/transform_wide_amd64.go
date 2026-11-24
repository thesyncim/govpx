//go:build goexperiment.simd && amd64 && !purego

package dsp

func inverseDCTWideGo(input []int16, dst []byte, stride, n, rows int) bool {
	switch n {
	case 8:
		return inverseDCT8AVX2(input, dst, stride, rows)
	case 16:
		return inverseDCT16AVX2(input, dst, stride, rows)
	}
	return false
}

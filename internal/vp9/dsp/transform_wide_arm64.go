//go:build goexperiment.simd && arm64 && !purego

package dsp

func inverseDCTWideGo(input []int16, dst []byte, stride, n, rows int) bool {
	if n == 8 {
		return inverseDCT8ARM(input, dst, stride, rows)
	}
	return false
}

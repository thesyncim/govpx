//go:build !arm64 || purego

package encoder

func hadamard8x8Into(src []int16, stride int, coeff []int16) {
	hadamard8x8Scalar(src, stride, coeff)
}

func hadamard16x16Into(src []int16, stride int, coeff []int16) {
	hadamard16x16Scalar(src, stride, coeff)
}

func satdAbsSum(coeff []int16, n int) int {
	return satdAbsSumScalar(coeff, n)
}

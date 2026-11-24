package encoder

import "github.com/thesyncim/govpx/internal/pixel"

func hadamard8x8Into(src []int16, stride int, coeff []int16) {
	if !hadamardGo(src, stride, coeff, 8) {
		hadamard8x8Scalar(src, stride, coeff)
	}
}

func hadamard16x16Into(src []int16, stride int, coeff []int16) {
	if !hadamardGo(src, stride, coeff, 16) {
		hadamard16x16Scalar(src, stride, coeff)
	}
}

func satdAbsSum(coeff []int16, n int) int {
	if pixel.Enabled {
		return pixel.AbsSum(coeff, n)
	}
	return satdAbsSumScalar(coeff, n)
}

// hadamardBlockYrd is only for 8-bit pixel differences. BlockYrd consumes
// coefficient-wise quantization followed by SATD and squared error, so any
// shared AC permutation is immaterial; DC must remain at coefficient zero.
// libvpx vpx_dsp/arm/hadamard_neon.c omits its final 8x8 transpose for this
// reason. The canonical Hadamard helpers retain their full int16 contract.
func hadamardBlockYrd(src []int16, stride int, coeff []int16, n int) {
	if hadamardBlockYrdGo(src, stride, coeff, n) {
		return
	}
	if n == 16 {
		hadamard16x16Scalar(src, stride, coeff)
	} else {
		hadamard8x8Scalar(src, stride, coeff)
	}
}

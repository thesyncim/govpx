//go:build arm64 && !purego

package encoder

import "unsafe"

// ARMv8 NEON dispatchers for the nonrd block_yrd Hadamard/SATD primitives.
// Kernels port libvpx v1.16.0 vpx_dsp/arm/hadamard_neon.c
// (vpx_hadamard_8x8_neon / vpx_hadamard_16x16_neon) and
// vpx_dsp/arm/avg_neon.c (vpx_satd_neon). Layouts that the kernels cannot
// address fall back to the canonical scalar ports.

func hadamard8x8Into(src []int16, stride int, coeff []int16) {
	if stride < 8 || len(src) < 7*stride+8 || len(coeff) < 64 {
		hadamard8x8Scalar(src, stride, coeff)
		return
	}
	hadamard8x8NEON(unsafe.SliceData(src), stride, unsafe.SliceData(coeff))
}

func hadamard16x16Into(src []int16, stride int, coeff []int16) {
	if stride < 16 || len(src) < 15*stride+16 || len(coeff) < 256 {
		hadamard16x16Scalar(src, stride, coeff)
		return
	}
	srcPtr := unsafe.SliceData(src)
	coeffPtr := unsafe.SliceData(coeff)
	// libvpx vpx_hadamard_16x16_neon: rearrange 16x16 to 8x32 via four 8x8
	// Hadamards, then combine with a halving butterfly over the four slabs.
	hadamard8x8NEON(srcPtr, stride, coeffPtr)
	hadamard8x8NEON(unsafe.SliceData(src[8:]), stride, unsafe.SliceData(coeff[64:]))
	hadamard8x8NEON(unsafe.SliceData(src[8*stride:]), stride, unsafe.SliceData(coeff[128:]))
	hadamard8x8NEON(unsafe.SliceData(src[8*stride+8:]), stride, unsafe.SliceData(coeff[192:]))
	hadamardCombine16NEON(coeffPtr)
}

func satdAbsSum(coeff []int16, n int) int {
	if n < 16 || n&15 != 0 || len(coeff) < n {
		return satdAbsSumScalar(coeff, n)
	}
	return int(satdNEON(unsafe.SliceData(coeff), n))
}

//go:noescape
func hadamard8x8NEON(src *int16, stride int, coeff *int16)

//go:noescape
func hadamardCombine16NEON(coeff *int16)

//go:noescape
func satdNEON(coeff *int16, n int) int64

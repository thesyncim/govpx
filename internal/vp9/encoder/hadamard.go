package encoder

// hadamardCol8 ports libvpx v1.16.0 vpx_dsp/avg.c hadamard_col8 verbatim:
// one 8-point Hadamard butterfly over a strided int16 column.
func hadamardCol8(src []int16, stride int, coeff []int16) {
	b0 := int(src[0*stride]) + int(src[1*stride])
	b1 := int(src[0*stride]) - int(src[1*stride])
	b2 := int(src[2*stride]) + int(src[3*stride])
	b3 := int(src[2*stride]) - int(src[3*stride])
	b4 := int(src[4*stride]) + int(src[5*stride])
	b5 := int(src[4*stride]) - int(src[5*stride])
	b6 := int(src[6*stride]) + int(src[7*stride])
	b7 := int(src[6*stride]) - int(src[7*stride])

	c0 := b0 + b2
	c1 := b1 + b3
	c2 := b0 - b2
	c3 := b1 - b3
	c4 := b4 + b6
	c5 := b5 + b7
	c6 := b4 - b6
	c7 := b5 - b7

	coeff[0] = int16(c0 + c4)
	coeff[7] = int16(c1 + c5)
	coeff[3] = int16(c2 + c6)
	coeff[4] = int16(c3 + c7)
	coeff[2] = int16(c0 - c4)
	coeff[6] = int16(c1 - c5)
	coeff[1] = int16(c2 - c6)
	coeff[5] = int16(c3 - c7)
}

// hadamard8x8Scalar ports libvpx v1.16.0 vpx_dsp/avg.c vpx_hadamard_8x8_c.
func hadamard8x8Scalar(src []int16, stride int, coeff []int16) {
	var buffer [64]int16
	var buffer2 [64]int16
	for idx := range 8 {
		hadamardCol8(src[idx:], stride, buffer[idx*8:])
	}
	for idx := range 8 {
		hadamardCol8(buffer[idx:], 8, buffer2[idx*8:])
	}
	copy(coeff[:64], buffer2[:])
}

// hadamard16x16Scalar ports libvpx v1.16.0 vpx_dsp/avg.c vpx_hadamard_16x16_c.
func hadamard16x16Scalar(src []int16, stride int, coeff []int16) {
	hadamard8x8Scalar(src, stride, coeff[:64])
	hadamard8x8Scalar(src[8:], stride, coeff[64:128])
	hadamard8x8Scalar(src[8*stride:], stride, coeff[128:192])
	hadamard8x8Scalar(src[8*stride+8:], stride, coeff[192:256])
	for idx := range 64 {
		a0 := int(coeff[idx])
		a1 := int(coeff[64+idx])
		a2 := int(coeff[128+idx])
		a3 := int(coeff[192+idx])

		b0 := (a0 + a1) >> 1
		b1 := (a0 - a1) >> 1
		b2 := (a2 + a3) >> 1
		b3 := (a2 - a3) >> 1

		coeff[idx] = int16(b0 + b2)
		coeff[64+idx] = int16(b1 + b3)
		coeff[128+idx] = int16(b0 - b2)
		coeff[192+idx] = int16(b1 - b3)
	}
}

// satdAbsSumScalar ports libvpx v1.16.0 vpx_dsp/avg.c vpx_satd_c: the sum of
// absolute values of the first n coefficients.
func satdAbsSumScalar(coeff []int16, n int) int {
	sum := 0
	for i := range n {
		v := int(coeff[i])
		if v < 0 {
			v = -v
		}
		sum += v
	}
	return sum
}

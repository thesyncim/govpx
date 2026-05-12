package dsp

// Iadst4 is the 4-point 1-D inverse ADST kernel used for intra 4x4
// blocks under the {ADST_DCT, DCT_ADST, ADST_ADST} transform types.
// Matches iadst4_c in libvpx v1.16.0 vpx_dsp/inv_txfm.c line-for-line:
// same sinpi multiplies, same combine pattern, same dct_const_round_shift.
func Iadst4(input, output []int32) {
	x0 := int64(int16(input[0]))
	x1 := int64(int16(input[1]))
	x2 := int64(int16(input[2]))
	x3 := int64(int16(input[3]))

	if x0|x1|x2|x3 == 0 {
		output[0] = 0
		output[1] = 0
		output[2] = 0
		output[3] = 0
		return
	}

	s0 := sinpi1_9 * x0
	s1 := sinpi2_9 * x0
	s2 := sinpi3_9 * x1
	s3 := sinpi4_9 * x2
	s4 := sinpi1_9 * x2
	s5 := sinpi2_9 * x3
	s6 := sinpi4_9 * x3
	s7 := int64(wrapLow(x0 - x2 + x3))

	s0 = s0 + s3 + s5
	s1 = s1 - s4 - s6
	s3 = s2
	s2 = sinpi3_9 * s7

	output[0] = wrapLow(int64(dctConstRoundShift(s0 + s3)))
	output[1] = wrapLow(int64(dctConstRoundShift(s1 + s3)))
	output[2] = wrapLow(int64(dctConstRoundShift(s2)))
	output[3] = wrapLow(int64(dctConstRoundShift(s0 + s1 - s3)))
}

// Iht4x4_16Add applies a 2-D inverse transform to a 4x4 coefficient
// block and adds the result onto dest. The transform pair is selected
// by TxType: (DCT_DCT, ADST_DCT, DCT_ADST, ADST_ADST). Mirrors
// vp9_iht4x4_16_add_c (which lives in vp9/common/vp9_idct.c upstream).
// We accept the 1-D row and column kernels as function values so the
// dispatcher can be kept out of this file.
func iht4x4_16Add(rowKernel, colKernel func(in, out []int32), input []int32, dest []uint8, stride int) {
	var out [16]int32
	for i := 0; i < 4; i++ {
		rowKernel(input[i*4:i*4+4], out[i*4:i*4+4])
	}
	var tempIn, tempOut [4]int32
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			tempIn[j] = out[j*4+i]
		}
		colKernel(tempIn[:], tempOut[:])
		for j := 0; j < 4; j++ {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(tempOut[j], 4))
		}
	}
}

// Iht4x4_16Add dispatches the inverse transform pair for a 4x4 intra
// block. txType is 0..3 in the order (DCT_DCT, ADST_DCT, DCT_ADST,
// ADST_ADST). Matches the dispatch in libvpx's vp9_iht4x4_16_add_c.
func Iht4x4_16Add(input []int32, dest []uint8, stride int, txType int) {
	switch txType {
	case 0: // DCT_DCT
		iht4x4_16Add(idct4, idct4, input, dest, stride)
	case 1: // ADST_DCT (ADST vertical, DCT horizontal): row pass DCT, col pass ADST
		iht4x4_16Add(idct4, Iadst4, input, dest, stride)
	case 2: // DCT_ADST (DCT vertical, ADST horizontal): row pass ADST, col pass DCT
		iht4x4_16Add(Iadst4, idct4, input, dest, stride)
	case 3: // ADST_ADST
		iht4x4_16Add(Iadst4, Iadst4, input, dest, stride)
	}
}

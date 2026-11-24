package dsp

// Iadst4 is the 4-point 1-D inverse ADST kernel used for intra 4x4
// blocks under the {ADST_DCT, DCT_ADST, ADST_ADST} transform types.
// Matches iadst4_c in libvpx v1.16.0 vpx_dsp/inv_txfm.c line-for-line.
func Iadst4(input, output []int16) {
	x0 := int64(input[0])
	x1 := int64(input[1])
	x2 := int64(input[2])
	x3 := int64(input[3])

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
	s7 := int64(int16(x0 - x2 + x3))

	s0 = s0 + s3 + s5
	s1 = s1 - s4 - s6
	s3 = s2
	s2 = sinpi3_9 * s7

	output[0] = int16(dctConstRoundShift(s0 + s3))
	output[1] = int16(dctConstRoundShift(s1 + s3))
	output[2] = int16(dctConstRoundShift(s2))
	output[3] = int16(dctConstRoundShift(s0 + s1 - s3))
}

// Iht4x4_16Add applies a 2-D inverse transform to a 4x4 coefficient
// block and adds the result onto dest. The transform pair is selected
// by txType: (DCT_DCT, ADST_DCT, DCT_ADST, ADST_ADST). Mirrors
// vp9_iht4x4_16_add_c.
func iht4x4_16Add(input []int16, dest []uint8, stride int, txType int) {
	rowAdst := txType == 2 || txType == 3
	colAdst := txType == 1 || txType == 3

	var out [16]int16
	for i := range 4 {
		if rowAdst {
			Iadst4(input[i*4:i*4+4], out[i*4:i*4+4])
		} else {
			idct4(input[i*4:i*4+4], out[i*4:i*4+4])
		}
	}
	var tempIn, tempOut [4]int16
	for i := range 4 {
		for j := range 4 {
			tempIn[j] = out[j*4+i]
		}
		if colAdst {
			Iadst4(tempIn[:], tempOut[:])
		} else {
			idct4(tempIn[:], tempOut[:])
		}
		for j := range 4 {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(int32(tempOut[j]), 4))
		}
	}
}

// iht4x4_16AddScalar dispatches the inverse transform pair for a 4x4
// intra block. txType is 0..3 in the order (DCT_DCT, ADST_DCT,
// DCT_ADST, ADST_ADST). Matches the dispatch in libvpx's
// vp9_iht4x4_16_add_c.
//
// Scalar reference; the exported Iht4x4_16Add wrapper is in
// idct_dispatch_*.go.
func iht4x4_16AddScalar(input []int16, dest []uint8, stride int, txType int) {
	iht4x4_16Add(input, dest, stride, txType)
}

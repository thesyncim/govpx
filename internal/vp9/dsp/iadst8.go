package dsp

// iadst8 is the 8-point 1-D inverse ADST used by intra 8x8 blocks under
// the ADST_DCT / DCT_ADST / ADST_ADST transform types. Matches iadst8_c
// in libvpx v1.16.0 vpx_dsp/inv_txfm.c line-for-line; intermediate
// stages cast through int (i.e. int32 in libvpx, modelled as int64
// here) and then store-truncate to int16 on every WRAPLOW.
func iadst8(input, output []int16) {
	x0 := int64(input[7])
	x1 := int64(input[0])
	x2 := int64(input[5])
	x3 := int64(input[2])
	x4 := int64(input[3])
	x5 := int64(input[4])
	x6 := int64(input[1])
	x7 := int64(input[6])

	if x0|x1|x2|x3|x4|x5|x6|x7 == 0 {
		for i := range 8 {
			output[i] = 0
		}
		return
	}

	// stage 1
	s0 := cospi2_64*x0 + cospi30_64*x1
	s1 := cospi30_64*x0 - cospi2_64*x1
	s2 := cospi10_64*x2 + cospi22_64*x3
	s3 := cospi22_64*x2 - cospi10_64*x3
	s4 := cospi18_64*x4 + cospi14_64*x5
	s5 := cospi14_64*x4 - cospi18_64*x5
	s6 := cospi26_64*x6 + cospi6_64*x7
	s7 := cospi6_64*x6 - cospi26_64*x7

	x0 = int64(int16(dctConstRoundShift(s0 + s4)))
	x1 = int64(int16(dctConstRoundShift(s1 + s5)))
	x2 = int64(int16(dctConstRoundShift(s2 + s6)))
	x3 = int64(int16(dctConstRoundShift(s3 + s7)))
	x4 = int64(int16(dctConstRoundShift(s0 - s4)))
	x5 = int64(int16(dctConstRoundShift(s1 - s5)))
	x6 = int64(int16(dctConstRoundShift(s2 - s6)))
	x7 = int64(int16(dctConstRoundShift(s3 - s7)))

	// stage 2
	s0 = x0
	s1 = x1
	s2 = x2
	s3 = x3
	s4 = cospi8_64*x4 + cospi24_64*x5
	s5 = cospi24_64*x4 - cospi8_64*x5
	s6 = -cospi24_64*x6 + cospi8_64*x7
	s7 = cospi8_64*x6 + cospi24_64*x7

	x0 = int64(int16(s0 + s2))
	x1 = int64(int16(s1 + s3))
	x2 = int64(int16(s0 - s2))
	x3 = int64(int16(s1 - s3))
	x4 = int64(int16(dctConstRoundShift(s4 + s6)))
	x5 = int64(int16(dctConstRoundShift(s5 + s7)))
	x6 = int64(int16(dctConstRoundShift(s4 - s6)))
	x7 = int64(int16(dctConstRoundShift(s5 - s7)))

	// stage 3
	s2 = cospi16_64 * (x2 + x3)
	s3 = cospi16_64 * (x2 - x3)
	s6 = cospi16_64 * (x6 + x7)
	s7 = cospi16_64 * (x6 - x7)

	x2 = int64(int16(dctConstRoundShift(s2)))
	x3 = int64(int16(dctConstRoundShift(s3)))
	x6 = int64(int16(dctConstRoundShift(s6)))
	x7 = int64(int16(dctConstRoundShift(s7)))

	output[0] = int16(x0)
	output[1] = -int16(x4)
	output[2] = int16(x6)
	output[3] = -int16(x2)
	output[4] = int16(x3)
	output[5] = -int16(x7)
	output[6] = int16(x5)
	output[7] = -int16(x1)
}

// iht8x8_64Add applies a 2-D inverse transform to an 8x8 coefficient
// block and adds the result onto dest. The transform pair is selected
// by txType: (DCT_DCT, ADST_DCT, DCT_ADST, ADST_ADST). Mirrors
// vp9_iht8x8_64_add_c.
func iht8x8_64Add(input []int16, dest []uint8, stride int, txType int) {
	rowAdst := txType == 2 || txType == 3
	colAdst := txType == 1 || txType == 3

	var out [64]int16
	for i := range 8 {
		if rowAdst {
			iadst8(input[i*8:i*8+8], out[i*8:i*8+8])
		} else {
			idct8(input[i*8:i*8+8], out[i*8:i*8+8])
		}
	}
	var tempIn, tempOut [8]int16
	for i := range 8 {
		for j := range 8 {
			tempIn[j] = out[j*8+i]
		}
		if colAdst {
			iadst8(tempIn[:], tempOut[:])
		} else {
			idct8(tempIn[:], tempOut[:])
		}
		for j := range 8 {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(int32(tempOut[j]), 5))
		}
	}
}

// iht8x8_64AddScalar dispatches the 2-D inverse transform for an 8x8
// intra block. txType is 0..3 in the order (DCT_DCT, ADST_DCT,
// DCT_ADST, ADST_ADST). Matches vp9_iht8x8_64_add_c.
//
// Scalar reference; the exported Iht8x8_64Add wrapper is in
// idct_dispatch_*.go.
func iht8x8_64AddScalar(input []int16, dest []uint8, stride int, txType int) {
	iht8x8_64Add(input, dest, stride, txType)
}

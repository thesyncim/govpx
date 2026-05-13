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

// iht8x8_64Add is the shared 2-D dispatch body for Iht8x8_64Add. The
// row and column kernels are passed in so the caller can compose any
// (DCT, ADST) pair without this function knowing the dispatch table.
func iht8x8_64Add(rowKernel, colKernel func(in, out []int16), input []int16, dest []uint8, stride int) {
	var out [64]int16
	for i := range 8 {
		rowKernel(input[i*8:i*8+8], out[i*8:i*8+8])
	}
	var tempIn, tempOut [8]int16
	for i := range 8 {
		for j := range 8 {
			tempIn[j] = out[j*8+i]
		}
		colKernel(tempIn[:], tempOut[:])
		for j := range 8 {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(int32(tempOut[j]), 5))
		}
	}
}

// Iht8x8_64Add dispatches the 2-D inverse transform for an 8x8 intra
// block. txType is 0..3 in the order (DCT_DCT, ADST_DCT, DCT_ADST,
// ADST_ADST). Matches vp9_iht8x8_64_add_c.
func Iht8x8_64Add(input []int16, dest []uint8, stride int, txType int) {
	switch txType {
	case 0: // DCT_DCT
		iht8x8_64Add(idct8, idct8, input, dest, stride)
	case 1: // ADST_DCT: row pass DCT, col pass ADST
		iht8x8_64Add(idct8, iadst8, input, dest, stride)
	case 2: // DCT_ADST: row pass ADST, col pass DCT
		iht8x8_64Add(iadst8, idct8, input, dest, stride)
	case 3: // ADST_ADST
		iht8x8_64Add(iadst8, iadst8, input, dest, stride)
	}
}

// Iadst8 is exported for the DSP oracle so test harnesses can probe the
// 1-D kernel directly. Application code drives it through Iht8x8_64Add.
func Iadst8(input, output []int16) { iadst8(input, output) }

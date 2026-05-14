package dsp

// iadst16 is the 16-point 1-D inverse ADST used by intra 16x16 blocks
// under the {ADST_DCT, DCT_ADST, ADST_ADST} transform types. Matches
// iadst16_c in libvpx v1.16.0 vpx_dsp/inv_txfm.c stage-by-stage; the
// bit-reverse input load + sign-mixed output store at the end are the
// libvpx fingerprint to preserve.
func iadst16(input, output []int16) {
	x0 := int64(input[15])
	x1 := int64(input[0])
	x2 := int64(input[13])
	x3 := int64(input[2])
	x4 := int64(input[11])
	x5 := int64(input[4])
	x6 := int64(input[9])
	x7 := int64(input[6])
	x8 := int64(input[7])
	x9 := int64(input[8])
	x10 := int64(input[5])
	x11 := int64(input[10])
	x12 := int64(input[3])
	x13 := int64(input[12])
	x14 := int64(input[1])
	x15 := int64(input[14])

	if x0|x1|x2|x3|x4|x5|x6|x7|x8|x9|x10|x11|x12|x13|x14|x15 == 0 {
		for i := range 16 {
			output[i] = 0
		}
		return
	}

	// stage 1
	s0 := x0*cospi1_64 + x1*cospi31_64
	s1 := x0*cospi31_64 - x1*cospi1_64
	s2 := x2*cospi5_64 + x3*cospi27_64
	s3 := x2*cospi27_64 - x3*cospi5_64
	s4 := x4*cospi9_64 + x5*cospi23_64
	s5 := x4*cospi23_64 - x5*cospi9_64
	s6 := x6*cospi13_64 + x7*cospi19_64
	s7 := x6*cospi19_64 - x7*cospi13_64
	s8 := x8*cospi17_64 + x9*cospi15_64
	s9 := x8*cospi15_64 - x9*cospi17_64
	s10 := x10*cospi21_64 + x11*cospi11_64
	s11 := x10*cospi11_64 - x11*cospi21_64
	s12 := x12*cospi25_64 + x13*cospi7_64
	s13 := x12*cospi7_64 - x13*cospi25_64
	s14 := x14*cospi29_64 + x15*cospi3_64
	s15 := x14*cospi3_64 - x15*cospi29_64

	x0 = int64(int16(dctConstRoundShift(s0 + s8)))
	x1 = int64(int16(dctConstRoundShift(s1 + s9)))
	x2 = int64(int16(dctConstRoundShift(s2 + s10)))
	x3 = int64(int16(dctConstRoundShift(s3 + s11)))
	x4 = int64(int16(dctConstRoundShift(s4 + s12)))
	x5 = int64(int16(dctConstRoundShift(s5 + s13)))
	x6 = int64(int16(dctConstRoundShift(s6 + s14)))
	x7 = int64(int16(dctConstRoundShift(s7 + s15)))
	x8 = int64(int16(dctConstRoundShift(s0 - s8)))
	x9 = int64(int16(dctConstRoundShift(s1 - s9)))
	x10 = int64(int16(dctConstRoundShift(s2 - s10)))
	x11 = int64(int16(dctConstRoundShift(s3 - s11)))
	x12 = int64(int16(dctConstRoundShift(s4 - s12)))
	x13 = int64(int16(dctConstRoundShift(s5 - s13)))
	x14 = int64(int16(dctConstRoundShift(s6 - s14)))
	x15 = int64(int16(dctConstRoundShift(s7 - s15)))

	// stage 2
	s0 = x0
	s1 = x1
	s2 = x2
	s3 = x3
	s4 = x4
	s5 = x5
	s6 = x6
	s7 = x7
	s8 = x8*cospi4_64 + x9*cospi28_64
	s9 = x8*cospi28_64 - x9*cospi4_64
	s10 = x10*cospi20_64 + x11*cospi12_64
	s11 = x10*cospi12_64 - x11*cospi20_64
	s12 = -x12*cospi28_64 + x13*cospi4_64
	s13 = x12*cospi4_64 + x13*cospi28_64
	s14 = -x14*cospi12_64 + x15*cospi20_64
	s15 = x14*cospi20_64 + x15*cospi12_64

	x0 = int64(int16(s0 + s4))
	x1 = int64(int16(s1 + s5))
	x2 = int64(int16(s2 + s6))
	x3 = int64(int16(s3 + s7))
	x4 = int64(int16(s0 - s4))
	x5 = int64(int16(s1 - s5))
	x6 = int64(int16(s2 - s6))
	x7 = int64(int16(s3 - s7))
	x8 = int64(int16(dctConstRoundShift(s8 + s12)))
	x9 = int64(int16(dctConstRoundShift(s9 + s13)))
	x10 = int64(int16(dctConstRoundShift(s10 + s14)))
	x11 = int64(int16(dctConstRoundShift(s11 + s15)))
	x12 = int64(int16(dctConstRoundShift(s8 - s12)))
	x13 = int64(int16(dctConstRoundShift(s9 - s13)))
	x14 = int64(int16(dctConstRoundShift(s10 - s14)))
	x15 = int64(int16(dctConstRoundShift(s11 - s15)))

	// stage 3
	s0 = x0
	s1 = x1
	s2 = x2
	s3 = x3
	s4 = x4*cospi8_64 + x5*cospi24_64
	s5 = x4*cospi24_64 - x5*cospi8_64
	s6 = -x6*cospi24_64 + x7*cospi8_64
	s7 = x6*cospi8_64 + x7*cospi24_64
	s8 = x8
	s9 = x9
	s10 = x10
	s11 = x11
	s12 = x12*cospi8_64 + x13*cospi24_64
	s13 = x12*cospi24_64 - x13*cospi8_64
	s14 = -x14*cospi24_64 + x15*cospi8_64
	s15 = x14*cospi8_64 + x15*cospi24_64

	x0 = int64(int16(s0 + s2))
	x1 = int64(int16(s1 + s3))
	x2 = int64(int16(s0 - s2))
	x3 = int64(int16(s1 - s3))
	x4 = int64(int16(dctConstRoundShift(s4 + s6)))
	x5 = int64(int16(dctConstRoundShift(s5 + s7)))
	x6 = int64(int16(dctConstRoundShift(s4 - s6)))
	x7 = int64(int16(dctConstRoundShift(s5 - s7)))
	x8 = int64(int16(s8 + s10))
	x9 = int64(int16(s9 + s11))
	x10 = int64(int16(s8 - s10))
	x11 = int64(int16(s9 - s11))
	x12 = int64(int16(dctConstRoundShift(s12 + s14)))
	x13 = int64(int16(dctConstRoundShift(s13 + s15)))
	x14 = int64(int16(dctConstRoundShift(s12 - s14)))
	x15 = int64(int16(dctConstRoundShift(s13 - s15)))

	// stage 4
	s2 = -cospi16_64 * (x2 + x3)
	s3 = cospi16_64 * (x2 - x3)
	s6 = cospi16_64 * (x6 + x7)
	s7 = cospi16_64 * (-x6 + x7)
	s10 = cospi16_64 * (x10 + x11)
	s11 = cospi16_64 * (-x10 + x11)
	s14 = -cospi16_64 * (x14 + x15)
	s15 = cospi16_64 * (x14 - x15)

	x2 = int64(int16(dctConstRoundShift(s2)))
	x3 = int64(int16(dctConstRoundShift(s3)))
	x6 = int64(int16(dctConstRoundShift(s6)))
	x7 = int64(int16(dctConstRoundShift(s7)))
	x10 = int64(int16(dctConstRoundShift(s10)))
	x11 = int64(int16(dctConstRoundShift(s11)))
	x14 = int64(int16(dctConstRoundShift(s14)))
	x15 = int64(int16(dctConstRoundShift(s15)))

	output[0] = int16(x0)
	output[1] = -int16(x8)
	output[2] = int16(x12)
	output[3] = -int16(x4)
	output[4] = int16(x6)
	output[5] = int16(x14)
	output[6] = int16(x10)
	output[7] = int16(x2)
	output[8] = int16(x3)
	output[9] = int16(x11)
	output[10] = int16(x15)
	output[11] = int16(x7)
	output[12] = int16(x5)
	output[13] = -int16(x13)
	output[14] = int16(x9)
	output[15] = -int16(x1)
}

// iht16x16_256Add is the shared 2-D dispatch body for hybrid DCT/ADST
// pairs. The transform type is switched inside the row and column passes so
// the caller does not pass function values through this hot path.
func iht16x16_256Add(txType int, input []int16, dest []uint8, stride int) {
	var out [256]int16
	for i := range 16 {
		switch txType {
		case 2, 3:
			iadst16(input[i*16:i*16+16], out[i*16:i*16+16])
		default:
			idct16(input[i*16:i*16+16], out[i*16:i*16+16])
		}
	}
	var tempIn, tempOut [16]int16
	for i := range 16 {
		for j := range 16 {
			tempIn[j] = out[j*16+i]
		}
		switch txType {
		case 1, 3:
			iadst16(tempIn[:], tempOut[:])
		default:
			idct16(tempIn[:], tempOut[:])
		}
		for j := range 16 {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(int32(tempOut[j]), 6))
		}
	}
}

// Iht16x16_256Add dispatches the 2-D inverse transform for a 16x16 intra
// block. txType is 0..3 in the order (DCT_DCT, ADST_DCT, DCT_ADST,
// ADST_ADST). Matches vp9_iht16x16_256_add_c.
func Iht16x16_256Add(input []int16, dest []uint8, stride int, txType int) {
	switch txType {
	case 0: // DCT_DCT
		idct16x16Add(input, dest, stride, 16)
	case 1: // ADST_DCT
		iht16x16_256Add(txType, input, dest, stride)
	case 2: // DCT_ADST
		iht16x16_256Add(txType, input, dest, stride)
	case 3: // ADST_ADST
		iht16x16_256Add(txType, input, dest, stride)
	}
}

// Iadst16 is exported for oracle harnesses.
func Iadst16(input, output []int16) { iadst16(input, output) }

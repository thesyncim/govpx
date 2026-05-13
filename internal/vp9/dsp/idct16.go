package dsp

// idct16 implements libvpx's idct16_c — the 16-point 1-D inverse DCT
// used by every Tx16x16 reconstruction. Seven-stage butterfly with the
// bit-reverse permutation applied at stage 1. Matches vpx_dsp/inv_txfm.c
// stage-by-stage; step1 / step2 are int16_t in libvpx so we narrow on
// every store to preserve byte parity.
func idct16(input, output []int16) {
	var step1, step2 [16]int16

	// stage 1 — bit-reverse permutation. Inputs are read as
	//   step1[k] = input[bitrev(k, 16) / 2]
	// matching the literal `input[0/2], input[16/2], ...` form in libvpx.
	step1[0] = input[0]
	step1[1] = input[8]
	step1[2] = input[4]
	step1[3] = input[12]
	step1[4] = input[2]
	step1[5] = input[10]
	step1[6] = input[6]
	step1[7] = input[14]
	step1[8] = input[1]
	step1[9] = input[9]
	step1[10] = input[5]
	step1[11] = input[13]
	step1[12] = input[3]
	step1[13] = input[11]
	step1[14] = input[7]
	step1[15] = input[15]

	// stage 2
	step2[0] = step1[0]
	step2[1] = step1[1]
	step2[2] = step1[2]
	step2[3] = step1[3]
	step2[4] = step1[4]
	step2[5] = step1[5]
	step2[6] = step1[6]
	step2[7] = step1[7]

	temp1 := int64(step1[8])*cospi30_64 - int64(step1[15])*cospi2_64
	temp2 := int64(step1[8])*cospi2_64 + int64(step1[15])*cospi30_64
	step2[8] = int16(dctConstRoundShift(temp1))
	step2[15] = int16(dctConstRoundShift(temp2))

	temp1 = int64(step1[9])*cospi14_64 - int64(step1[14])*cospi18_64
	temp2 = int64(step1[9])*cospi18_64 + int64(step1[14])*cospi14_64
	step2[9] = int16(dctConstRoundShift(temp1))
	step2[14] = int16(dctConstRoundShift(temp2))

	temp1 = int64(step1[10])*cospi22_64 - int64(step1[13])*cospi10_64
	temp2 = int64(step1[10])*cospi10_64 + int64(step1[13])*cospi22_64
	step2[10] = int16(dctConstRoundShift(temp1))
	step2[13] = int16(dctConstRoundShift(temp2))

	temp1 = int64(step1[11])*cospi6_64 - int64(step1[12])*cospi26_64
	temp2 = int64(step1[11])*cospi26_64 + int64(step1[12])*cospi6_64
	step2[11] = int16(dctConstRoundShift(temp1))
	step2[12] = int16(dctConstRoundShift(temp2))

	// stage 3
	step1[0] = step2[0]
	step1[1] = step2[1]
	step1[2] = step2[2]
	step1[3] = step2[3]

	temp1 = int64(step2[4])*cospi28_64 - int64(step2[7])*cospi4_64
	temp2 = int64(step2[4])*cospi4_64 + int64(step2[7])*cospi28_64
	step1[4] = int16(dctConstRoundShift(temp1))
	step1[7] = int16(dctConstRoundShift(temp2))
	temp1 = int64(step2[5])*cospi12_64 - int64(step2[6])*cospi20_64
	temp2 = int64(step2[5])*cospi20_64 + int64(step2[6])*cospi12_64
	step1[5] = int16(dctConstRoundShift(temp1))
	step1[6] = int16(dctConstRoundShift(temp2))

	step1[8] = int16(int64(step2[8]) + int64(step2[9]))
	step1[9] = int16(int64(step2[8]) - int64(step2[9]))
	step1[10] = int16(-int64(step2[10]) + int64(step2[11]))
	step1[11] = int16(int64(step2[10]) + int64(step2[11]))
	step1[12] = int16(int64(step2[12]) + int64(step2[13]))
	step1[13] = int16(int64(step2[12]) - int64(step2[13]))
	step1[14] = int16(-int64(step2[14]) + int64(step2[15]))
	step1[15] = int16(int64(step2[14]) + int64(step2[15]))

	// stage 4
	temp1 = (int64(step1[0]) + int64(step1[1])) * cospi16_64
	temp2 = (int64(step1[0]) - int64(step1[1])) * cospi16_64
	step2[0] = int16(dctConstRoundShift(temp1))
	step2[1] = int16(dctConstRoundShift(temp2))
	temp1 = int64(step1[2])*cospi24_64 - int64(step1[3])*cospi8_64
	temp2 = int64(step1[2])*cospi8_64 + int64(step1[3])*cospi24_64
	step2[2] = int16(dctConstRoundShift(temp1))
	step2[3] = int16(dctConstRoundShift(temp2))
	step2[4] = int16(int64(step1[4]) + int64(step1[5]))
	step2[5] = int16(int64(step1[4]) - int64(step1[5]))
	step2[6] = int16(-int64(step1[6]) + int64(step1[7]))
	step2[7] = int16(int64(step1[6]) + int64(step1[7]))

	step2[8] = step1[8]
	step2[15] = step1[15]
	temp1 = -int64(step1[9])*cospi8_64 + int64(step1[14])*cospi24_64
	temp2 = int64(step1[9])*cospi24_64 + int64(step1[14])*cospi8_64
	step2[9] = int16(dctConstRoundShift(temp1))
	step2[14] = int16(dctConstRoundShift(temp2))
	temp1 = -int64(step1[10])*cospi24_64 - int64(step1[13])*cospi8_64
	temp2 = -int64(step1[10])*cospi8_64 + int64(step1[13])*cospi24_64
	step2[10] = int16(dctConstRoundShift(temp1))
	step2[13] = int16(dctConstRoundShift(temp2))
	step2[11] = step1[11]
	step2[12] = step1[12]

	// stage 5
	step1[0] = int16(int64(step2[0]) + int64(step2[3]))
	step1[1] = int16(int64(step2[1]) + int64(step2[2]))
	step1[2] = int16(int64(step2[1]) - int64(step2[2]))
	step1[3] = int16(int64(step2[0]) - int64(step2[3]))
	step1[4] = step2[4]
	temp1 = (int64(step2[6]) - int64(step2[5])) * cospi16_64
	temp2 = (int64(step2[5]) + int64(step2[6])) * cospi16_64
	step1[5] = int16(dctConstRoundShift(temp1))
	step1[6] = int16(dctConstRoundShift(temp2))
	step1[7] = step2[7]

	step1[8] = int16(int64(step2[8]) + int64(step2[11]))
	step1[9] = int16(int64(step2[9]) + int64(step2[10]))
	step1[10] = int16(int64(step2[9]) - int64(step2[10]))
	step1[11] = int16(int64(step2[8]) - int64(step2[11]))
	step1[12] = int16(-int64(step2[12]) + int64(step2[15]))
	step1[13] = int16(-int64(step2[13]) + int64(step2[14]))
	step1[14] = int16(int64(step2[13]) + int64(step2[14]))
	step1[15] = int16(int64(step2[12]) + int64(step2[15]))

	// stage 6
	step2[0] = int16(int64(step1[0]) + int64(step1[7]))
	step2[1] = int16(int64(step1[1]) + int64(step1[6]))
	step2[2] = int16(int64(step1[2]) + int64(step1[5]))
	step2[3] = int16(int64(step1[3]) + int64(step1[4]))
	step2[4] = int16(int64(step1[3]) - int64(step1[4]))
	step2[5] = int16(int64(step1[2]) - int64(step1[5]))
	step2[6] = int16(int64(step1[1]) - int64(step1[6]))
	step2[7] = int16(int64(step1[0]) - int64(step1[7]))
	step2[8] = step1[8]
	step2[9] = step1[9]
	temp1 = (-int64(step1[10]) + int64(step1[13])) * cospi16_64
	temp2 = (int64(step1[10]) + int64(step1[13])) * cospi16_64
	step2[10] = int16(dctConstRoundShift(temp1))
	step2[13] = int16(dctConstRoundShift(temp2))
	temp1 = (-int64(step1[11]) + int64(step1[12])) * cospi16_64
	temp2 = (int64(step1[11]) + int64(step1[12])) * cospi16_64
	step2[11] = int16(dctConstRoundShift(temp1))
	step2[12] = int16(dctConstRoundShift(temp2))
	step2[14] = step1[14]
	step2[15] = step1[15]

	// stage 7
	output[0] = int16(int64(step2[0]) + int64(step2[15]))
	output[1] = int16(int64(step2[1]) + int64(step2[14]))
	output[2] = int16(int64(step2[2]) + int64(step2[13]))
	output[3] = int16(int64(step2[3]) + int64(step2[12]))
	output[4] = int16(int64(step2[4]) + int64(step2[11]))
	output[5] = int16(int64(step2[5]) + int64(step2[10]))
	output[6] = int16(int64(step2[6]) + int64(step2[9]))
	output[7] = int16(int64(step2[7]) + int64(step2[8]))
	output[8] = int16(int64(step2[7]) - int64(step2[8]))
	output[9] = int16(int64(step2[6]) - int64(step2[9]))
	output[10] = int16(int64(step2[5]) - int64(step2[10]))
	output[11] = int16(int64(step2[4]) - int64(step2[11]))
	output[12] = int16(int64(step2[3]) - int64(step2[12]))
	output[13] = int16(int64(step2[2]) - int64(step2[13]))
	output[14] = int16(int64(step2[1]) - int64(step2[14]))
	output[15] = int16(int64(step2[0]) - int64(step2[15]))
}

// idct16x16Add is the shared body for the 256/38/10 add wrappers below.
// rowLimit caps how many input rows are run through the row pass; the
// out[] buffer is left zero for rows beyond that, matching libvpx's
// sparse fast paths.
func idct16x16Add(input []int16, dest []uint8, stride, rowLimit int) {
	var out [256]int16
	for i := range rowLimit {
		idct16(input[i*16:i*16+16], out[i*16:i*16+16])
	}
	var tempIn, tempOut [16]int16
	for i := range 16 {
		for j := range 16 {
			tempIn[j] = out[j*16+i]
		}
		idct16(tempIn[:], tempOut[:])
		for j := range 16 {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(int32(tempOut[j]), 6))
		}
	}
}

// Idct16x16_256Add applies the full 16x16 inverse DCT and adds it onto
// dest. Mirrors vpx_idct16x16_256_add_c. Row + column pass, normalized
// by ROUND_POWER_OF_TWO(., 6).
func Idct16x16_256Add(input []int16, dest []uint8, stride int) {
	idct16x16Add(input, dest, stride, 16)
}

// Idct16x16_38Add is the sparse fast path where all non-zero
// coefficients sit in the upper-left 8x8 area. Mirrors
// vpx_idct16x16_38_add_c.
func Idct16x16_38Add(input []int16, dest []uint8, stride int) {
	idct16x16Add(input, dest, stride, 8)
}

// Idct16x16_10Add is the sparser fast path where all non-zero
// coefficients sit in the upper-left 4x4 area. Mirrors
// vpx_idct16x16_10_add_c.
func Idct16x16_10Add(input []int16, dest []uint8, stride int) {
	idct16x16Add(input, dest, stride, 4)
}

// Idct16x16_1Add is the DC-only fast path. Matches vpx_idct16x16_1_add_c.
func Idct16x16_1Add(input []int16, dest []uint8, stride int) {
	out := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
	out = int16(dctConstRoundShift(int64(out) * cospi16_64))
	a1 := roundPowerOfTwo(int32(out), 6)
	for j := range 16 {
		row := j * stride
		for i := range 16 {
			dest[row+i] = clipPixelAdd(dest[row+i], a1)
		}
	}
}

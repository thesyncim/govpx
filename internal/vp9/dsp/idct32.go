package dsp

// idct32 implements libvpx's idct32_c — the 32-point 1-D inverse DCT
// used by every Tx32x32 reconstruction. Seven butterfly stages with
// bit-reverse-permuted input at stage 1; step1 / step2 are int16_t in
// libvpx, so we narrow on every store to preserve byte parity.
func idct32(input, output []int16) {
	var step1, step2 [32]int16

	// stage 1
	step1[0] = input[0]
	step1[1] = input[16]
	step1[2] = input[8]
	step1[3] = input[24]
	step1[4] = input[4]
	step1[5] = input[20]
	step1[6] = input[12]
	step1[7] = input[28]
	step1[8] = input[2]
	step1[9] = input[18]
	step1[10] = input[10]
	step1[11] = input[26]
	step1[12] = input[6]
	step1[13] = input[22]
	step1[14] = input[14]
	step1[15] = input[30]

	temp1 := int64(input[1])*cospi31_64 - int64(input[31])*cospi1_64
	temp2 := int64(input[1])*cospi1_64 + int64(input[31])*cospi31_64
	step1[16] = int16(dctConstRoundShift(temp1))
	step1[31] = int16(dctConstRoundShift(temp2))

	temp1 = int64(input[17])*cospi15_64 - int64(input[15])*cospi17_64
	temp2 = int64(input[17])*cospi17_64 + int64(input[15])*cospi15_64
	step1[17] = int16(dctConstRoundShift(temp1))
	step1[30] = int16(dctConstRoundShift(temp2))

	temp1 = int64(input[9])*cospi23_64 - int64(input[23])*cospi9_64
	temp2 = int64(input[9])*cospi9_64 + int64(input[23])*cospi23_64
	step1[18] = int16(dctConstRoundShift(temp1))
	step1[29] = int16(dctConstRoundShift(temp2))

	temp1 = int64(input[25])*cospi7_64 - int64(input[7])*cospi25_64
	temp2 = int64(input[25])*cospi25_64 + int64(input[7])*cospi7_64
	step1[19] = int16(dctConstRoundShift(temp1))
	step1[28] = int16(dctConstRoundShift(temp2))

	temp1 = int64(input[5])*cospi27_64 - int64(input[27])*cospi5_64
	temp2 = int64(input[5])*cospi5_64 + int64(input[27])*cospi27_64
	step1[20] = int16(dctConstRoundShift(temp1))
	step1[27] = int16(dctConstRoundShift(temp2))

	temp1 = int64(input[21])*cospi11_64 - int64(input[11])*cospi21_64
	temp2 = int64(input[21])*cospi21_64 + int64(input[11])*cospi11_64
	step1[21] = int16(dctConstRoundShift(temp1))
	step1[26] = int16(dctConstRoundShift(temp2))

	temp1 = int64(input[13])*cospi19_64 - int64(input[19])*cospi13_64
	temp2 = int64(input[13])*cospi13_64 + int64(input[19])*cospi19_64
	step1[22] = int16(dctConstRoundShift(temp1))
	step1[25] = int16(dctConstRoundShift(temp2))

	temp1 = int64(input[29])*cospi3_64 - int64(input[3])*cospi29_64
	temp2 = int64(input[29])*cospi29_64 + int64(input[3])*cospi3_64
	step1[23] = int16(dctConstRoundShift(temp1))
	step1[24] = int16(dctConstRoundShift(temp2))

	// stage 2
	step2[0] = step1[0]
	step2[1] = step1[1]
	step2[2] = step1[2]
	step2[3] = step1[3]
	step2[4] = step1[4]
	step2[5] = step1[5]
	step2[6] = step1[6]
	step2[7] = step1[7]

	temp1 = int64(step1[8])*cospi30_64 - int64(step1[15])*cospi2_64
	temp2 = int64(step1[8])*cospi2_64 + int64(step1[15])*cospi30_64
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

	step2[16] = int16(int64(step1[16]) + int64(step1[17]))
	step2[17] = int16(int64(step1[16]) - int64(step1[17]))
	step2[18] = int16(-int64(step1[18]) + int64(step1[19]))
	step2[19] = int16(int64(step1[18]) + int64(step1[19]))
	step2[20] = int16(int64(step1[20]) + int64(step1[21]))
	step2[21] = int16(int64(step1[20]) - int64(step1[21]))
	step2[22] = int16(-int64(step1[22]) + int64(step1[23]))
	step2[23] = int16(int64(step1[22]) + int64(step1[23]))
	step2[24] = int16(int64(step1[24]) + int64(step1[25]))
	step2[25] = int16(int64(step1[24]) - int64(step1[25]))
	step2[26] = int16(-int64(step1[26]) + int64(step1[27]))
	step2[27] = int16(int64(step1[26]) + int64(step1[27]))
	step2[28] = int16(int64(step1[28]) + int64(step1[29]))
	step2[29] = int16(int64(step1[28]) - int64(step1[29]))
	step2[30] = int16(-int64(step1[30]) + int64(step1[31]))
	step2[31] = int16(int64(step1[30]) + int64(step1[31]))

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

	step1[16] = step2[16]
	step1[31] = step2[31]
	temp1 = -int64(step2[17])*cospi4_64 + int64(step2[30])*cospi28_64
	temp2 = int64(step2[17])*cospi28_64 + int64(step2[30])*cospi4_64
	step1[17] = int16(dctConstRoundShift(temp1))
	step1[30] = int16(dctConstRoundShift(temp2))
	temp1 = -int64(step2[18])*cospi28_64 - int64(step2[29])*cospi4_64
	temp2 = -int64(step2[18])*cospi4_64 + int64(step2[29])*cospi28_64
	step1[18] = int16(dctConstRoundShift(temp1))
	step1[29] = int16(dctConstRoundShift(temp2))
	step1[19] = step2[19]
	step1[20] = step2[20]
	temp1 = -int64(step2[21])*cospi20_64 + int64(step2[26])*cospi12_64
	temp2 = int64(step2[21])*cospi12_64 + int64(step2[26])*cospi20_64
	step1[21] = int16(dctConstRoundShift(temp1))
	step1[26] = int16(dctConstRoundShift(temp2))
	temp1 = -int64(step2[22])*cospi12_64 - int64(step2[25])*cospi20_64
	temp2 = -int64(step2[22])*cospi20_64 + int64(step2[25])*cospi12_64
	step1[22] = int16(dctConstRoundShift(temp1))
	step1[25] = int16(dctConstRoundShift(temp2))
	step1[23] = step2[23]
	step1[24] = step2[24]
	step1[27] = step2[27]
	step1[28] = step2[28]

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

	step2[16] = int16(int64(step1[16]) + int64(step1[19]))
	step2[17] = int16(int64(step1[17]) + int64(step1[18]))
	step2[18] = int16(int64(step1[17]) - int64(step1[18]))
	step2[19] = int16(int64(step1[16]) - int64(step1[19]))
	step2[20] = int16(-int64(step1[20]) + int64(step1[23]))
	step2[21] = int16(-int64(step1[21]) + int64(step1[22]))
	step2[22] = int16(int64(step1[21]) + int64(step1[22]))
	step2[23] = int16(int64(step1[20]) + int64(step1[23]))

	step2[24] = int16(int64(step1[24]) + int64(step1[27]))
	step2[25] = int16(int64(step1[25]) + int64(step1[26]))
	step2[26] = int16(int64(step1[25]) - int64(step1[26]))
	step2[27] = int16(int64(step1[24]) - int64(step1[27]))
	step2[28] = int16(-int64(step1[28]) + int64(step1[31]))
	step2[29] = int16(-int64(step1[29]) + int64(step1[30]))
	step2[30] = int16(int64(step1[29]) + int64(step1[30]))
	step2[31] = int16(int64(step1[28]) + int64(step1[31]))

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

	step1[16] = step2[16]
	step1[17] = step2[17]
	temp1 = -int64(step2[18])*cospi8_64 + int64(step2[29])*cospi24_64
	temp2 = int64(step2[18])*cospi24_64 + int64(step2[29])*cospi8_64
	step1[18] = int16(dctConstRoundShift(temp1))
	step1[29] = int16(dctConstRoundShift(temp2))
	temp1 = -int64(step2[19])*cospi8_64 + int64(step2[28])*cospi24_64
	temp2 = int64(step2[19])*cospi24_64 + int64(step2[28])*cospi8_64
	step1[19] = int16(dctConstRoundShift(temp1))
	step1[28] = int16(dctConstRoundShift(temp2))
	temp1 = -int64(step2[20])*cospi24_64 - int64(step2[27])*cospi8_64
	temp2 = -int64(step2[20])*cospi8_64 + int64(step2[27])*cospi24_64
	step1[20] = int16(dctConstRoundShift(temp1))
	step1[27] = int16(dctConstRoundShift(temp2))
	temp1 = -int64(step2[21])*cospi24_64 - int64(step2[26])*cospi8_64
	temp2 = -int64(step2[21])*cospi8_64 + int64(step2[26])*cospi24_64
	step1[21] = int16(dctConstRoundShift(temp1))
	step1[26] = int16(dctConstRoundShift(temp2))
	step1[22] = step2[22]
	step1[23] = step2[23]
	step1[24] = step2[24]
	step1[25] = step2[25]
	step1[30] = step2[30]
	step1[31] = step2[31]

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

	step2[16] = int16(int64(step1[16]) + int64(step1[23]))
	step2[17] = int16(int64(step1[17]) + int64(step1[22]))
	step2[18] = int16(int64(step1[18]) + int64(step1[21]))
	step2[19] = int16(int64(step1[19]) + int64(step1[20]))
	step2[20] = int16(int64(step1[19]) - int64(step1[20]))
	step2[21] = int16(int64(step1[18]) - int64(step1[21]))
	step2[22] = int16(int64(step1[17]) - int64(step1[22]))
	step2[23] = int16(int64(step1[16]) - int64(step1[23]))

	step2[24] = int16(-int64(step1[24]) + int64(step1[31]))
	step2[25] = int16(-int64(step1[25]) + int64(step1[30]))
	step2[26] = int16(-int64(step1[26]) + int64(step1[29]))
	step2[27] = int16(-int64(step1[27]) + int64(step1[28]))
	step2[28] = int16(int64(step1[27]) + int64(step1[28]))
	step2[29] = int16(int64(step1[26]) + int64(step1[29]))
	step2[30] = int16(int64(step1[25]) + int64(step1[30]))
	step2[31] = int16(int64(step1[24]) + int64(step1[31]))

	// stage 7
	step1[0] = int16(int64(step2[0]) + int64(step2[15]))
	step1[1] = int16(int64(step2[1]) + int64(step2[14]))
	step1[2] = int16(int64(step2[2]) + int64(step2[13]))
	step1[3] = int16(int64(step2[3]) + int64(step2[12]))
	step1[4] = int16(int64(step2[4]) + int64(step2[11]))
	step1[5] = int16(int64(step2[5]) + int64(step2[10]))
	step1[6] = int16(int64(step2[6]) + int64(step2[9]))
	step1[7] = int16(int64(step2[7]) + int64(step2[8]))
	step1[8] = int16(int64(step2[7]) - int64(step2[8]))
	step1[9] = int16(int64(step2[6]) - int64(step2[9]))
	step1[10] = int16(int64(step2[5]) - int64(step2[10]))
	step1[11] = int16(int64(step2[4]) - int64(step2[11]))
	step1[12] = int16(int64(step2[3]) - int64(step2[12]))
	step1[13] = int16(int64(step2[2]) - int64(step2[13]))
	step1[14] = int16(int64(step2[1]) - int64(step2[14]))
	step1[15] = int16(int64(step2[0]) - int64(step2[15]))

	step1[16] = step2[16]
	step1[17] = step2[17]
	step1[18] = step2[18]
	step1[19] = step2[19]
	temp1 = (-int64(step2[20]) + int64(step2[27])) * cospi16_64
	temp2 = (int64(step2[20]) + int64(step2[27])) * cospi16_64
	step1[20] = int16(dctConstRoundShift(temp1))
	step1[27] = int16(dctConstRoundShift(temp2))
	temp1 = (-int64(step2[21]) + int64(step2[26])) * cospi16_64
	temp2 = (int64(step2[21]) + int64(step2[26])) * cospi16_64
	step1[21] = int16(dctConstRoundShift(temp1))
	step1[26] = int16(dctConstRoundShift(temp2))
	temp1 = (-int64(step2[22]) + int64(step2[25])) * cospi16_64
	temp2 = (int64(step2[22]) + int64(step2[25])) * cospi16_64
	step1[22] = int16(dctConstRoundShift(temp1))
	step1[25] = int16(dctConstRoundShift(temp2))
	temp1 = (-int64(step2[23]) + int64(step2[24])) * cospi16_64
	temp2 = (int64(step2[23]) + int64(step2[24])) * cospi16_64
	step1[23] = int16(dctConstRoundShift(temp1))
	step1[24] = int16(dctConstRoundShift(temp2))
	step1[28] = step2[28]
	step1[29] = step2[29]
	step1[30] = step2[30]
	step1[31] = step2[31]

	// final stage
	output[0] = int16(int64(step1[0]) + int64(step1[31]))
	output[1] = int16(int64(step1[1]) + int64(step1[30]))
	output[2] = int16(int64(step1[2]) + int64(step1[29]))
	output[3] = int16(int64(step1[3]) + int64(step1[28]))
	output[4] = int16(int64(step1[4]) + int64(step1[27]))
	output[5] = int16(int64(step1[5]) + int64(step1[26]))
	output[6] = int16(int64(step1[6]) + int64(step1[25]))
	output[7] = int16(int64(step1[7]) + int64(step1[24]))
	output[8] = int16(int64(step1[8]) + int64(step1[23]))
	output[9] = int16(int64(step1[9]) + int64(step1[22]))
	output[10] = int16(int64(step1[10]) + int64(step1[21]))
	output[11] = int16(int64(step1[11]) + int64(step1[20]))
	output[12] = int16(int64(step1[12]) + int64(step1[19]))
	output[13] = int16(int64(step1[13]) + int64(step1[18]))
	output[14] = int16(int64(step1[14]) + int64(step1[17]))
	output[15] = int16(int64(step1[15]) + int64(step1[16]))
	output[16] = int16(int64(step1[15]) - int64(step1[16]))
	output[17] = int16(int64(step1[14]) - int64(step1[17]))
	output[18] = int16(int64(step1[13]) - int64(step1[18]))
	output[19] = int16(int64(step1[12]) - int64(step1[19]))
	output[20] = int16(int64(step1[11]) - int64(step1[20]))
	output[21] = int16(int64(step1[10]) - int64(step1[21]))
	output[22] = int16(int64(step1[9]) - int64(step1[22]))
	output[23] = int16(int64(step1[8]) - int64(step1[23]))
	output[24] = int16(int64(step1[7]) - int64(step1[24]))
	output[25] = int16(int64(step1[6]) - int64(step1[25]))
	output[26] = int16(int64(step1[5]) - int64(step1[26]))
	output[27] = int16(int64(step1[4]) - int64(step1[27]))
	output[28] = int16(int64(step1[3]) - int64(step1[28]))
	output[29] = int16(int64(step1[2]) - int64(step1[29]))
	output[30] = int16(int64(step1[1]) - int64(step1[30]))
	output[31] = int16(int64(step1[0]) - int64(step1[31]))
}

// idct32x32Add is the shared body for the 1024/135/34 add wrappers. The
// rowLimit caps the row pass for the sparse fast paths. Matches the
// vpx_idct32x32_{1024,135,34}_add_c structure in vpx_dsp/inv_txfm.c —
// each variant zero-fills the out[] beyond the limit, then runs the
// full column pass.
func idct32x32Add(input []int16, dest []uint8, stride, rowLimit int) {
	var out [1024]int16
	for i := 0; i < rowLimit; i++ {
		idct32(input[i*32:i*32+32], out[i*32:i*32+32])
	}
	var tempIn, tempOut [32]int16
	for i := 0; i < 32; i++ {
		for j := 0; j < 32; j++ {
			tempIn[j] = out[j*32+i]
		}
		idct32(tempIn[:], tempOut[:])
		for j := 0; j < 32; j++ {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(int32(tempOut[j]), 6))
		}
	}
}

// Idct32x32_1024Add is the dense 32x32 add. Mirrors vpx_idct32x32_1024_add_c.
func Idct32x32_1024Add(input []int16, dest []uint8, stride int) {
	idct32x32Add(input, dest, stride, 32)
}

// Idct32x32_135Add is the sparse-top-left-16x16 fast path. Mirrors
// vpx_idct32x32_135_add_c.
func Idct32x32_135Add(input []int16, dest []uint8, stride int) {
	idct32x32Add(input, dest, stride, 16)
}

// Idct32x32_34Add is the sparser top-left-8x8 fast path. Mirrors
// vpx_idct32x32_34_add_c.
func Idct32x32_34Add(input []int16, dest []uint8, stride int) {
	idct32x32Add(input, dest, stride, 8)
}

// Idct32x32_1Add is the DC-only fast path. Mirrors vpx_idct32x32_1_add_c.
func Idct32x32_1Add(input []int16, dest []uint8, stride int) {
	out := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
	out = int16(dctConstRoundShift(int64(out) * cospi16_64))
	a1 := roundPowerOfTwo(int32(out), 6)
	for j := 0; j < 32; j++ {
		row := j * stride
		for i := 0; i < 32; i++ {
			dest[row+i] = clipPixelAdd(dest[row+i], a1)
		}
	}
}

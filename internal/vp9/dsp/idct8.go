package dsp

// idct8 implements libvpx's idct8_c — the 8-point 1-D inverse DCT used
// by every Tx8x8 reconstruction. Four-stage butterfly with cospi
// multiplies at stages 1, 2 and 3; stages collapse into the final
// output via additions only. Matches vpx_dsp/inv_txfm.c stage-by-stage.
func idct8(input, output []int32) {
	var step1, step2 [8]int32
	in0 := int64(int16(input[0]))
	in1 := int64(int16(input[1]))
	in2 := int64(int16(input[2]))
	in3 := int64(int16(input[3]))
	in4 := int64(int16(input[4]))
	in5 := int64(int16(input[5]))
	in6 := int64(int16(input[6]))
	in7 := int64(int16(input[7]))

	// stage 1
	step1[0] = int32(in0)
	step1[2] = int32(in4)
	step1[1] = int32(in2)
	step1[3] = int32(in6)
	temp1 := in1*cospi28_64 - in7*cospi4_64
	temp2 := in1*cospi4_64 + in7*cospi28_64
	step1[4] = wrapLow(int64(dctConstRoundShift(temp1)))
	step1[7] = wrapLow(int64(dctConstRoundShift(temp2)))
	temp1 = in5*cospi12_64 - in3*cospi20_64
	temp2 = in5*cospi20_64 + in3*cospi12_64
	step1[5] = wrapLow(int64(dctConstRoundShift(temp1)))
	step1[6] = wrapLow(int64(dctConstRoundShift(temp2)))

	// stage 2
	temp1 = int64(step1[0]+step1[2]) * cospi16_64
	temp2 = int64(step1[0]-step1[2]) * cospi16_64
	step2[0] = wrapLow(int64(dctConstRoundShift(temp1)))
	step2[1] = wrapLow(int64(dctConstRoundShift(temp2)))
	temp1 = int64(step1[1])*cospi24_64 - int64(step1[3])*cospi8_64
	temp2 = int64(step1[1])*cospi8_64 + int64(step1[3])*cospi24_64
	step2[2] = wrapLow(int64(dctConstRoundShift(temp1)))
	step2[3] = wrapLow(int64(dctConstRoundShift(temp2)))
	step2[4] = wrapLow(int64(step1[4] + step1[5]))
	step2[5] = wrapLow(int64(step1[4] - step1[5]))
	step2[6] = wrapLow(int64(-step1[6] + step1[7]))
	step2[7] = wrapLow(int64(step1[6] + step1[7]))

	// stage 3
	step1[0] = wrapLow(int64(step2[0] + step2[3]))
	step1[1] = wrapLow(int64(step2[1] + step2[2]))
	step1[2] = wrapLow(int64(step2[1] - step2[2]))
	step1[3] = wrapLow(int64(step2[0] - step2[3]))
	step1[4] = step2[4]
	temp1 = int64(step2[6]-step2[5]) * cospi16_64
	temp2 = int64(step2[5]+step2[6]) * cospi16_64
	step1[5] = wrapLow(int64(dctConstRoundShift(temp1)))
	step1[6] = wrapLow(int64(dctConstRoundShift(temp2)))
	step1[7] = step2[7]

	// stage 4
	output[0] = wrapLow(int64(step1[0] + step1[7]))
	output[1] = wrapLow(int64(step1[1] + step1[6]))
	output[2] = wrapLow(int64(step1[2] + step1[5]))
	output[3] = wrapLow(int64(step1[3] + step1[4]))
	output[4] = wrapLow(int64(step1[3] - step1[4]))
	output[5] = wrapLow(int64(step1[2] - step1[5]))
	output[6] = wrapLow(int64(step1[1] - step1[6]))
	output[7] = wrapLow(int64(step1[0] - step1[7]))
}

// Idct8x8_64Add applies the full 8x8 inverse DCT to a 64-coefficient
// block and adds the result onto dest. Mirrors vpx_idct8x8_64_add_c:
// row pass into a transposed intermediate, then a column pass with
// ROUND_POWER_OF_TWO(., 5) into clip_pixel_add.
func Idct8x8_64Add(input []int32, dest []uint8, stride int) {
	var out [64]int32
	for i := 0; i < 8; i++ {
		idct8(input[i*8:i*8+8], out[i*8:i*8+8])
	}
	var tempIn, tempOut [8]int32
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			tempIn[j] = out[j*8+i]
		}
		idct8(tempIn[:], tempOut[:])
		for j := 0; j < 8; j++ {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(tempOut[j], 5))
		}
	}
}

// Idct8x8_12Add is the sparse fast path used when only the top-left
// 4x4 of the input is non-zero. Matches vpx_idct8x8_12_add_c: it skips
// the lower 4 rows of the row pass and zero-fills out before the column
// pass. The output is the same as Idct8x8_64Add when input[8..63] are
// all zero in the non-top-left positions.
func Idct8x8_12Add(input []int32, dest []uint8, stride int) {
	var out [64]int32
	for i := 0; i < 4; i++ {
		idct8(input[i*8:i*8+8], out[i*8:i*8+8])
	}
	var tempIn, tempOut [8]int32
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			tempIn[j] = out[j*8+i]
		}
		idct8(tempIn[:], tempOut[:])
		for j := 0; j < 8; j++ {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(tempOut[j], 5))
		}
	}
}

// Idct8x8_1Add is the DC-only fast path. Matches vpx_idct8x8_1_add_c.
func Idct8x8_1Add(input []int32, dest []uint8, stride int) {
	out := wrapLow(int64(dctConstRoundShift(int64(int16(input[0])) * cospi16_64)))
	out = wrapLow(int64(dctConstRoundShift(int64(out) * cospi16_64)))
	a1 := roundPowerOfTwo(out, 5)
	for j := 0; j < 8; j++ {
		row := j * stride
		for i := 0; i < 8; i++ {
			dest[row+i] = clipPixelAdd(dest[row+i], a1)
		}
	}
}

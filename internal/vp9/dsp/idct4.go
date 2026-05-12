package dsp

// idct4 implements libvpx's idct4_c: a 4-point 1-D inverse DCT. Input
// and output are 4 element slices; the caller passes a stride of 1
// element. Matches the line-by-line structure of vpx_dsp/inv_txfm.c so
// any future intermediate-precision tweak made upstream is easy to
// mirror.
func idct4(input, output []int32) {
	var step [4]int32
	in0 := int64(int16(input[0]))
	in1 := int64(int16(input[1]))
	in2 := int64(int16(input[2]))
	in3 := int64(int16(input[3]))

	// stage 1
	temp1 := (in0 + in2) * cospi16_64
	temp2 := (in0 - in2) * cospi16_64
	step[0] = wrapLow(int64(dctConstRoundShift(temp1)))
	step[1] = wrapLow(int64(dctConstRoundShift(temp2)))
	temp1 = in1*cospi24_64 - in3*cospi8_64
	temp2 = in1*cospi8_64 + in3*cospi24_64
	step[2] = wrapLow(int64(dctConstRoundShift(temp1)))
	step[3] = wrapLow(int64(dctConstRoundShift(temp2)))

	// stage 2
	output[0] = wrapLow(int64(step[0] + step[3]))
	output[1] = wrapLow(int64(step[1] + step[2]))
	output[2] = wrapLow(int64(step[1] - step[2]))
	output[3] = wrapLow(int64(step[0] - step[3]))
}

// Idct4x4_16Add applies the full 4x4 inverse DCT to a 16-coefficient
// block and adds the result onto the dest pixels at the given stride.
// Mirrors vpx_idct4x4_16_add_c: row pass produces a 4x4 intermediate,
// then a column pass with the >>4 normalization shift folded into the
// pixel-add via clip_pixel_add(ROUND_POWER_OF_TWO(., 4)).
func Idct4x4_16Add(input []int32, dest []uint8, stride int) {
	var out [16]int32
	for i := 0; i < 4; i++ {
		idct4(input[i*4:i*4+4], out[i*4:i*4+4])
	}
	var tempIn, tempOut [4]int32
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			tempIn[j] = out[j*4+i]
		}
		idct4(tempIn[:], tempOut[:])
		for j := 0; j < 4; j++ {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(tempOut[j], 4))
		}
	}
}

// Idct4x4_1Add handles the common case where only the DC coefficient is
// non-zero. Matches vpx_idct4x4_1_add_c — two cospi_16_64 multiplies
// followed by a single +>>4 broadcast across the 4x4 pixel block.
func Idct4x4_1Add(input []int32, dest []uint8, stride int) {
	out := wrapLow(int64(dctConstRoundShift(int64(int16(input[0])) * cospi16_64)))
	out = wrapLow(int64(dctConstRoundShift(int64(out) * cospi16_64)))
	a1 := roundPowerOfTwo(out, 4)

	for i := 0; i < 4; i++ {
		row := i * stride
		dest[row+0] = clipPixelAdd(dest[row+0], a1)
		dest[row+1] = clipPixelAdd(dest[row+1], a1)
		dest[row+2] = clipPixelAdd(dest[row+2], a1)
		dest[row+3] = clipPixelAdd(dest[row+3], a1)
	}
}

package dsp

// idct4 implements libvpx's idct4_c: a 4-point 1-D inverse DCT. Input
// and output are 4 element slices; the caller passes a stride of 1
// element. Matches the line-by-line structure of vpx_dsp/inv_txfm.c so
// any future intermediate-precision tweak made upstream is easy to
// mirror.
//
// The output slice type is []int16 to match libvpx's tran_low_t in the
// 8-bit (non-highbitdepth) configuration. Intermediate computation is
// done in int64 to model the tran_high_t accumulator.
func idct4(input, output []int16) {
	var step [4]int32
	in0 := int64(input[0])
	in1 := int64(input[1])
	in2 := int64(input[2])
	in3 := int64(input[3])

	// stage 1
	temp1 := (in0 + in2) * cospi16_64
	temp2 := (in0 - in2) * cospi16_64
	step[0] = int32(dctConstRoundShift(temp1))
	step[1] = int32(dctConstRoundShift(temp2))
	temp1 = in1*cospi24_64 - in3*cospi8_64
	temp2 = in1*cospi8_64 + in3*cospi24_64
	step[2] = int32(dctConstRoundShift(temp1))
	step[3] = int32(dctConstRoundShift(temp2))

	// stage 2 — the libvpx output buffer is int16_t, so the int32 sums
	// are truncated to int16 on store. The govpx ports must do the same
	// for byte parity.
	output[0] = int16(step[0] + step[3])
	output[1] = int16(step[1] + step[2])
	output[2] = int16(step[1] - step[2])
	output[3] = int16(step[0] - step[3])
}

// idct4x4_16AddScalar applies the full 4x4 inverse DCT to a
// 16-coefficient block and adds the result onto the dest pixels at the
// given stride. Mirrors vpx_idct4x4_16_add_c: row pass produces a 4x4
// intermediate, then a column pass with the >>4 normalization shift
// folded into the pixel-add via clip_pixel_add(ROUND_POWER_OF_TWO(.,
// 4)).
//
// This is the always-available scalar reference. The exported
// Idct4x4_16Add wrapper lives in idct_dispatch_*.go and routes either
// here or to a NEON kernel.
func idct4x4_16AddScalar(input []int16, dest []uint8, stride int) {
	var out [16]int16
	for i := range 4 {
		idct4(input[i*4:i*4+4], out[i*4:i*4+4])
	}
	var tempIn, tempOut [4]int16
	for i := range 4 {
		for j := range 4 {
			tempIn[j] = out[j*4+i]
		}
		idct4(tempIn[:], tempOut[:])
		for j := range 4 {
			pos := j*stride + i
			dest[pos] = clipPixelAdd(dest[pos], roundPowerOfTwo(int32(tempOut[j]), 4))
		}
	}
}

// idct4x4_1AddScalar handles the common case where only the DC
// coefficient is non-zero. Matches vpx_idct4x4_1_add_c — two
// cospi_16_64 multiplies followed by a single +>>4 broadcast across the
// 4x4 pixel block.
//
// This is the always-available scalar reference. The exported
// Idct4x4_1Add wrapper lives in idct_dispatch_*.go.
func idct4x4_1AddScalar(input []int16, dest []uint8, stride int) {
	out := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
	out = int16(dctConstRoundShift(int64(out) * cospi16_64))
	a1 := roundPowerOfTwo(int32(out), 4)

	for i := range 4 {
		row := i * stride
		dest[row+0] = clipPixelAdd(dest[row+0], a1)
		dest[row+1] = clipPixelAdd(dest[row+1], a1)
		dest[row+2] = clipPixelAdd(dest[row+2], a1)
		dest[row+3] = clipPixelAdd(dest[row+3], a1)
	}
}

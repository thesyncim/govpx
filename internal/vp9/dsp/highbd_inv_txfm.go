package dsp

import "github.com/thesyncim/govpx/internal/vp9/common"

// High-bit-depth (10-bit / 12-bit) VP9 inverse transforms.
// Ported verbatim from libvpx v1.16.0 vpx_dsp/inv_txfm.c.

func highbdClipPixelAdd(dest uint16, trans int32, bd int) uint16 {
	return clipPixelHighbd(int(dest)+int(trans), bd)
}

func highbdRoundShift(val int32, bits uint) int32 {
	return (val + (1 << (bits - 1))) >> bits
}

func highbdIwht4x4Add(input []int32, dest []uint16, dstOff, stride, eob, bd int) {
	if eob <= 1 {
		a1 := input[0] >> unitQuantShift
		e1 := a1 >> 1
		a1 -= e1
		var tmp [4]int32
		tmp[0] = a1
		tmp[1] = e1
		tmp[2] = e1
		tmp[3] = e1
		for i := range 4 {
			e := tmp[i] >> 1
			a := tmp[i] - e
			dest[dstOff+0*stride+i] = highbdClipPixelAdd(dest[dstOff+0*stride+i], a, bd)
			dest[dstOff+1*stride+i] = highbdClipPixelAdd(dest[dstOff+1*stride+i], e, bd)
			dest[dstOff+2*stride+i] = highbdClipPixelAdd(dest[dstOff+2*stride+i], e, bd)
			dest[dstOff+3*stride+i] = highbdClipPixelAdd(dest[dstOff+3*stride+i], e, bd)
		}
		return
	}
	var output [16]int32
	for i := range 4 {
		in := input[i*4:]
		a1 := in[0] >> unitQuantShift
		c1 := in[1] >> unitQuantShift
		d1 := in[2] >> unitQuantShift
		b1 := in[3] >> unitQuantShift
		a1 += c1
		d1 -= b1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= b1
		d1 += c1
		output[i*4+0] = a1
		output[i*4+1] = b1
		output[i*4+2] = c1
		output[i*4+3] = d1
	}
	for i := range 4 {
		a1 := output[0*4+i]
		c1 := output[1*4+i]
		d1 := output[2*4+i]
		b1 := output[3*4+i]
		a1 += c1
		d1 -= b1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= b1
		d1 += c1
		dest[dstOff+0*stride+i] = highbdClipPixelAdd(dest[dstOff+0*stride+i], a1, bd)
		dest[dstOff+1*stride+i] = highbdClipPixelAdd(dest[dstOff+1*stride+i], b1, bd)
		dest[dstOff+2*stride+i] = highbdClipPixelAdd(dest[dstOff+2*stride+i], c1, bd)
		dest[dstOff+3*stride+i] = highbdClipPixelAdd(dest[dstOff+3*stride+i], d1, bd)
	}
}

func highbdIdct4(input, output []int32) {
	var step [4]int32
	t1 := (int64(input[0]) + int64(input[2])) * cospi16_64
	t2 := (int64(input[0]) - int64(input[2])) * cospi16_64
	step[0] = dctConstRoundShift(t1)
	step[1] = dctConstRoundShift(t2)
	t1 = int64(input[1])*cospi24_64 - int64(input[3])*cospi8_64
	t2 = int64(input[1])*cospi8_64 + int64(input[3])*cospi24_64
	step[2] = dctConstRoundShift(t1)
	step[3] = dctConstRoundShift(t2)

	output[0] = step[0] + step[3]
	output[1] = step[1] + step[2]
	output[2] = step[1] - step[2]
	output[3] = step[0] - step[3]
}

func highbdIadst4(input, output []int32) {
	x0, x1, x2, x3 := input[0], input[1], input[2], input[3]
	if x0|x1|x2|x3 == 0 {
		output[0], output[1], output[2], output[3] = 0, 0, 0, 0
		return
	}
	s0 := int64(sinpi1_9) * int64(x0)
	s1 := int64(sinpi2_9) * int64(x0)
	s2 := int64(sinpi3_9) * int64(x1)
	s3 := int64(sinpi4_9) * int64(x2)
	s4 := int64(sinpi1_9) * int64(x2)
	s5 := int64(sinpi2_9) * int64(x3)
	s6 := int64(sinpi4_9) * int64(x3)
	s7 := int64(x0 - x2 + x3)

	s0 = s0 + s3 + s5
	s1 = s1 - s4 - s6
	s3 = s2
	s2 = int64(sinpi3_9) * s7

	output[0] = dctConstRoundShift(s0 + s3)
	output[1] = dctConstRoundShift(s1 + s3)
	output[2] = dctConstRoundShift(s2)
	output[3] = dctConstRoundShift(s0 + s1 - s3)
}

func highbdIdct8(input, output []int32) {
	var step1, step2 [8]int32
	step1[0] = input[0]
	step1[2] = input[4]
	step1[1] = input[2]
	step1[3] = input[6]
	t1 := int64(input[1])*cospi28_64 - int64(input[7])*cospi4_64
	t2 := int64(input[1])*cospi4_64 + int64(input[7])*cospi28_64
	step1[4] = dctConstRoundShift(t1)
	step1[7] = dctConstRoundShift(t2)
	t1 = int64(input[5])*cospi12_64 - int64(input[3])*cospi20_64
	t2 = int64(input[5])*cospi20_64 + int64(input[3])*cospi12_64
	step1[5] = dctConstRoundShift(t1)
	step1[6] = dctConstRoundShift(t2)

	highbdIdct4(step1[:4], step1[:4])

	step2[4] = step1[4] + step1[5]
	step2[5] = step1[4] - step1[5]
	step2[6] = -step1[6] + step1[7]
	step2[7] = step1[6] + step1[7]

	step1[4] = step2[4]
	t1 = int64(step2[6]-step2[5]) * cospi16_64
	t2 = int64(step2[5]+step2[6]) * cospi16_64
	step1[5] = dctConstRoundShift(t1)
	step1[6] = dctConstRoundShift(t2)
	step1[7] = step2[7]

	output[0] = step1[0] + step1[7]
	output[1] = step1[1] + step1[6]
	output[2] = step1[2] + step1[5]
	output[3] = step1[3] + step1[4]
	output[4] = step1[3] - step1[4]
	output[5] = step1[2] - step1[5]
	output[6] = step1[1] - step1[6]
	output[7] = step1[0] - step1[7]
}

func highbdIadst8(input, output []int32) {
	x0, x1, x2, x3 := input[7], input[0], input[5], input[2]
	x4, x5, x6, x7 := input[3], input[4], input[1], input[6]
	if x0|x1|x2|x3|x4|x5|x6|x7 == 0 {
		for i := range 8 {
			output[i] = 0
		}
		return
	}
	s0 := int64(cospi2_64)*int64(x0) + int64(cospi30_64)*int64(x1)
	s1 := int64(cospi30_64)*int64(x0) - int64(cospi2_64)*int64(x1)
	s2 := int64(cospi10_64)*int64(x2) + int64(cospi22_64)*int64(x3)
	s3 := int64(cospi22_64)*int64(x2) - int64(cospi10_64)*int64(x3)
	s4 := int64(cospi18_64)*int64(x4) + int64(cospi14_64)*int64(x5)
	s5 := int64(cospi14_64)*int64(x4) - int64(cospi18_64)*int64(x5)
	s6 := int64(cospi26_64)*int64(x6) + int64(cospi6_64)*int64(x7)
	s7 := int64(cospi6_64)*int64(x6) - int64(cospi26_64)*int64(x7)

	x0 = dctConstRoundShift(s0 + s4)
	x1 = dctConstRoundShift(s1 + s5)
	x2 = dctConstRoundShift(s2 + s6)
	x3 = dctConstRoundShift(s3 + s7)
	x4 = dctConstRoundShift(s0 - s4)
	x5 = dctConstRoundShift(s1 - s5)
	x6 = dctConstRoundShift(s2 - s6)
	x7 = dctConstRoundShift(s3 - s7)

	s0, s1, s2, s3 = int64(x0), int64(x1), int64(x2), int64(x3)
	s4 = int64(cospi8_64)*int64(x4) + int64(cospi24_64)*int64(x5)
	s5 = int64(cospi24_64)*int64(x4) - int64(cospi8_64)*int64(x5)
	s6 = int64(-cospi24_64)*int64(x6) + int64(cospi8_64)*int64(x7)
	s7 = int64(cospi8_64)*int64(x6) + int64(cospi24_64)*int64(x7)

	x0 = int32(s0 + s2)
	x1 = int32(s1 + s3)
	x2 = int32(s0 - s2)
	x3 = int32(s1 - s3)
	x4 = dctConstRoundShift(s4 + s6)
	x5 = dctConstRoundShift(s5 + s7)
	x6 = dctConstRoundShift(s4 - s6)
	x7 = dctConstRoundShift(s5 - s7)

	x2, x3 = dctConstRoundShift(int64(cospi16_64)*int64(x2+x3)), dctConstRoundShift(int64(cospi16_64)*int64(x2-x3))
	x6, x7 = dctConstRoundShift(int64(cospi16_64)*int64(x6+x7)), dctConstRoundShift(int64(cospi16_64)*int64(x6-x7))

	output[0] = x0
	output[1] = -x4
	output[2] = x6
	output[3] = -x2
	output[4] = x3
	output[5] = -x7
	output[6] = x5
	output[7] = -x1
}

func highbdIdct16(input, output []int32) {
	var step1, step2 [16]int32
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

	copy(step2[:8], step1[:8])
	step2[8] = dctConstRoundShift(int64(step1[8])*cospi30_64 - int64(step1[15])*cospi2_64)
	step2[15] = dctConstRoundShift(int64(step1[8])*cospi2_64 + int64(step1[15])*cospi30_64)
	step2[9] = dctConstRoundShift(int64(step1[9])*cospi14_64 - int64(step1[14])*cospi18_64)
	step2[14] = dctConstRoundShift(int64(step1[9])*cospi18_64 + int64(step1[14])*cospi14_64)
	step2[10] = dctConstRoundShift(int64(step1[10])*cospi22_64 - int64(step1[13])*cospi10_64)
	step2[13] = dctConstRoundShift(int64(step1[10])*cospi10_64 + int64(step1[13])*cospi22_64)
	step2[11] = dctConstRoundShift(int64(step1[11])*cospi6_64 - int64(step1[12])*cospi26_64)
	step2[12] = dctConstRoundShift(int64(step1[11])*cospi26_64 + int64(step1[12])*cospi6_64)

	copy(step1[:4], step2[:4])
	step1[4] = dctConstRoundShift(int64(step2[4])*cospi28_64 - int64(step2[7])*cospi4_64)
	step1[7] = dctConstRoundShift(int64(step2[4])*cospi4_64 + int64(step2[7])*cospi28_64)
	step1[5] = dctConstRoundShift(int64(step2[5])*cospi12_64 - int64(step2[6])*cospi20_64)
	step1[6] = dctConstRoundShift(int64(step2[5])*cospi20_64 + int64(step2[6])*cospi12_64)
	step1[8] = step2[8] + step2[9]
	step1[9] = step2[8] - step2[9]
	step1[10] = -step2[10] + step2[11]
	step1[11] = step2[10] + step2[11]
	step1[12] = step2[12] + step2[13]
	step1[13] = step2[12] - step2[13]
	step1[14] = -step2[14] + step2[15]
	step1[15] = step2[14] + step2[15]

	step2[0] = dctConstRoundShift(int64(step1[0]+step1[1]) * cospi16_64)
	step2[1] = dctConstRoundShift(int64(step1[0]-step1[1]) * cospi16_64)
	step2[2] = dctConstRoundShift(int64(step1[2])*cospi24_64 - int64(step1[3])*cospi8_64)
	step2[3] = dctConstRoundShift(int64(step1[2])*cospi8_64 + int64(step1[3])*cospi24_64)
	step2[4] = step1[4] + step1[5]
	step2[5] = step1[4] - step1[5]
	step2[6] = -step1[6] + step1[7]
	step2[7] = step1[6] + step1[7]
	step2[8] = step1[8]
	step2[15] = step1[15]
	step2[9] = dctConstRoundShift(-int64(step1[9])*cospi8_64 + int64(step1[14])*cospi24_64)
	step2[14] = dctConstRoundShift(int64(step1[9])*cospi24_64 + int64(step1[14])*cospi8_64)
	step2[10] = dctConstRoundShift(-int64(step1[10])*cospi24_64 - int64(step1[13])*cospi8_64)
	step2[13] = dctConstRoundShift(-int64(step1[10])*cospi8_64 + int64(step1[13])*cospi24_64)
	step2[11] = step1[11]
	step2[12] = step1[12]

	step1[0] = step2[0] + step2[3]
	step1[1] = step2[1] + step2[2]
	step1[2] = step2[1] - step2[2]
	step1[3] = step2[0] - step2[3]
	step1[4] = step2[4]
	step1[5] = dctConstRoundShift(int64(step2[6]-step2[5]) * cospi16_64)
	step1[6] = dctConstRoundShift(int64(step2[5]+step2[6]) * cospi16_64)
	step1[7] = step2[7]
	step1[8] = step2[8] + step2[11]
	step1[9] = step2[9] + step2[10]
	step1[10] = step2[9] - step2[10]
	step1[11] = step2[8] - step2[11]
	step1[12] = -step2[12] + step2[15]
	step1[13] = -step2[13] + step2[14]
	step1[14] = step2[13] + step2[14]
	step1[15] = step2[12] + step2[15]

	step2[0] = step1[0] + step1[7]
	step2[1] = step1[1] + step1[6]
	step2[2] = step1[2] + step1[5]
	step2[3] = step1[3] + step1[4]
	step2[4] = step1[3] - step1[4]
	step2[5] = step1[2] - step1[5]
	step2[6] = step1[1] - step1[6]
	step2[7] = step1[0] - step1[7]
	step2[8] = step1[8]
	step2[9] = step1[9]
	step2[10] = dctConstRoundShift(int64(-step1[10]+step1[13]) * cospi16_64)
	step2[13] = dctConstRoundShift(int64(step1[10]+step1[13]) * cospi16_64)
	step2[11] = dctConstRoundShift(int64(-step1[11]+step1[12]) * cospi16_64)
	step2[12] = dctConstRoundShift(int64(step1[11]+step1[12]) * cospi16_64)
	step2[14] = step1[14]
	step2[15] = step1[15]

	for i := range 8 {
		output[i] = step2[i] + step2[15-i]
		output[15-i] = step2[i] - step2[15-i]
	}
}

func highbdIadst16(input, output []int32) {
	x0, x1, x2, x3 := input[15], input[0], input[13], input[2]
	x4, x5, x6, x7 := input[11], input[4], input[9], input[6]
	x8, x9, x10, x11 := input[7], input[8], input[5], input[10]
	x12, x13, x14, x15 := input[3], input[12], input[1], input[14]
	if x0|x1|x2|x3|x4|x5|x6|x7|x8|x9|x10|x11|x12|x13|x14|x15 == 0 {
		for i := range 16 {
			output[i] = 0
		}
		return
	}
	s0 := int64(x0)*cospi1_64 + int64(x1)*cospi31_64
	s1 := int64(x0)*cospi31_64 - int64(x1)*cospi1_64
	s2 := int64(x2)*cospi5_64 + int64(x3)*cospi27_64
	s3 := int64(x2)*cospi27_64 - int64(x3)*cospi5_64
	s4 := int64(x4)*cospi9_64 + int64(x5)*cospi23_64
	s5 := int64(x4)*cospi23_64 - int64(x5)*cospi9_64
	s6 := int64(x6)*cospi13_64 + int64(x7)*cospi19_64
	s7 := int64(x6)*cospi19_64 - int64(x7)*cospi13_64
	s8 := int64(x8)*cospi17_64 + int64(x9)*cospi15_64
	s9 := int64(x8)*cospi15_64 - int64(x9)*cospi17_64
	s10 := int64(x10)*cospi21_64 + int64(x11)*cospi11_64
	s11 := int64(x10)*cospi11_64 - int64(x11)*cospi21_64
	s12 := int64(x12)*cospi25_64 + int64(x13)*cospi7_64
	s13 := int64(x12)*cospi7_64 - int64(x13)*cospi25_64
	s14 := int64(x14)*cospi29_64 + int64(x15)*cospi3_64
	s15 := int64(x14)*cospi3_64 - int64(x15)*cospi29_64

	x0 = dctConstRoundShift(s0 + s8)
	x1 = dctConstRoundShift(s1 + s9)
	x2 = dctConstRoundShift(s2 + s10)
	x3 = dctConstRoundShift(s3 + s11)
	x4 = dctConstRoundShift(s4 + s12)
	x5 = dctConstRoundShift(s5 + s13)
	x6 = dctConstRoundShift(s6 + s14)
	x7 = dctConstRoundShift(s7 + s15)
	x8 = dctConstRoundShift(s0 - s8)
	x9 = dctConstRoundShift(s1 - s9)
	x10 = dctConstRoundShift(s2 - s10)
	x11 = dctConstRoundShift(s3 - s11)
	x12 = dctConstRoundShift(s4 - s12)
	x13 = dctConstRoundShift(s5 - s13)
	x14 = dctConstRoundShift(s6 - s14)
	x15 = dctConstRoundShift(s7 - s15)

	s0, s1, s2, s3 = int64(x0), int64(x1), int64(x2), int64(x3)
	s4, s5, s6, s7 = int64(x4), int64(x5), int64(x6), int64(x7)
	s8 = int64(x8)*cospi4_64 + int64(x9)*cospi28_64
	s9 = int64(x8)*cospi28_64 - int64(x9)*cospi4_64
	s10 = int64(x10)*cospi20_64 + int64(x11)*cospi12_64
	s11 = int64(x10)*cospi12_64 - int64(x11)*cospi20_64
	s12 = -int64(x12)*cospi28_64 + int64(x13)*cospi4_64
	s13 = int64(x12)*cospi4_64 + int64(x13)*cospi28_64
	s14 = -int64(x14)*cospi12_64 + int64(x15)*cospi20_64
	s15 = int64(x14)*cospi20_64 + int64(x15)*cospi12_64

	x0 = int32(s0 + s4)
	x1 = int32(s1 + s5)
	x2 = int32(s2 + s6)
	x3 = int32(s3 + s7)
	x4 = int32(s0 - s4)
	x5 = int32(s1 - s5)
	x6 = int32(s2 - s6)
	x7 = int32(s3 - s7)
	x8 = dctConstRoundShift(s8 + s12)
	x9 = dctConstRoundShift(s9 + s13)
	x10 = dctConstRoundShift(s10 + s14)
	x11 = dctConstRoundShift(s11 + s15)
	x12 = dctConstRoundShift(s8 - s12)
	x13 = dctConstRoundShift(s9 - s13)
	x14 = dctConstRoundShift(s10 - s14)
	x15 = dctConstRoundShift(s11 - s15)

	s0, s1, s2, s3 = int64(x0), int64(x1), int64(x2), int64(x3)
	s4 = int64(x4)*cospi8_64 + int64(x5)*cospi24_64
	s5 = int64(x4)*cospi24_64 - int64(x5)*cospi8_64
	s6 = -int64(x6)*cospi24_64 + int64(x7)*cospi8_64
	s7 = int64(x6)*cospi8_64 + int64(x7)*cospi24_64
	s8, s9, s10, s11 = int64(x8), int64(x9), int64(x10), int64(x11)
	s12 = int64(x12)*cospi8_64 + int64(x13)*cospi24_64
	s13 = int64(x12)*cospi24_64 - int64(x13)*cospi8_64
	s14 = -int64(x14)*cospi24_64 + int64(x15)*cospi8_64
	s15 = int64(x14)*cospi8_64 + int64(x15)*cospi24_64

	x0 = int32(s0 + s2)
	x1 = int32(s1 + s3)
	x2 = int32(s0 - s2)
	x3 = int32(s1 - s3)
	x4 = dctConstRoundShift(s4 + s6)
	x5 = dctConstRoundShift(s5 + s7)
	x6 = dctConstRoundShift(s4 - s6)
	x7 = dctConstRoundShift(s5 - s7)
	x8 = int32(s8 + s10)
	x9 = int32(s9 + s11)
	x10 = int32(s8 - s10)
	x11 = int32(s9 - s11)
	x12 = dctConstRoundShift(s12 + s14)
	x13 = dctConstRoundShift(s13 + s15)
	x14 = dctConstRoundShift(s12 - s14)
	x15 = dctConstRoundShift(s13 - s15)

	x2, x3 = dctConstRoundShift(int64(-cospi16_64)*int64(x2+x3)), dctConstRoundShift(int64(cospi16_64)*int64(x2-x3))
	x6, x7 = dctConstRoundShift(int64(cospi16_64)*int64(x6+x7)), dctConstRoundShift(int64(cospi16_64)*int64(-x6+x7))
	x10, x11 = dctConstRoundShift(int64(cospi16_64)*int64(x10+x11)), dctConstRoundShift(int64(cospi16_64)*int64(-x10+x11))
	x14, x15 = dctConstRoundShift(int64(-cospi16_64)*int64(x14+x15)), dctConstRoundShift(int64(cospi16_64)*int64(x14-x15))

	output[0] = x0
	output[1] = -x8
	output[2] = x12
	output[3] = -x4
	output[4] = x6
	output[5] = x14
	output[6] = x10
	output[7] = x2
	output[8] = x3
	output[9] = x11
	output[10] = x15
	output[11] = x7
	output[12] = x5
	output[13] = -x13
	output[14] = x9
	output[15] = -x1
}

func highbdIdct32(input, output []int32) {
	var step1, step2 [32]int32
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

	step1[16] = dctConstRoundShift(int64(input[1])*cospi31_64 - int64(input[31])*cospi1_64)
	step1[31] = dctConstRoundShift(int64(input[1])*cospi1_64 + int64(input[31])*cospi31_64)
	step1[17] = dctConstRoundShift(int64(input[17])*cospi15_64 - int64(input[15])*cospi17_64)
	step1[30] = dctConstRoundShift(int64(input[17])*cospi17_64 + int64(input[15])*cospi15_64)
	step1[18] = dctConstRoundShift(int64(input[9])*cospi23_64 - int64(input[23])*cospi9_64)
	step1[29] = dctConstRoundShift(int64(input[9])*cospi9_64 + int64(input[23])*cospi23_64)
	step1[19] = dctConstRoundShift(int64(input[25])*cospi7_64 - int64(input[7])*cospi25_64)
	step1[28] = dctConstRoundShift(int64(input[25])*cospi25_64 + int64(input[7])*cospi7_64)
	step1[20] = dctConstRoundShift(int64(input[5])*cospi27_64 - int64(input[27])*cospi5_64)
	step1[27] = dctConstRoundShift(int64(input[5])*cospi5_64 + int64(input[27])*cospi27_64)
	step1[21] = dctConstRoundShift(int64(input[21])*cospi11_64 - int64(input[11])*cospi21_64)
	step1[26] = dctConstRoundShift(int64(input[21])*cospi21_64 + int64(input[11])*cospi11_64)
	step1[22] = dctConstRoundShift(int64(input[13])*cospi19_64 - int64(input[19])*cospi13_64)
	step1[25] = dctConstRoundShift(int64(input[13])*cospi13_64 + int64(input[19])*cospi19_64)
	step1[23] = dctConstRoundShift(int64(input[29])*cospi3_64 - int64(input[3])*cospi29_64)
	step1[24] = dctConstRoundShift(int64(input[29])*cospi29_64 + int64(input[3])*cospi3_64)

	copy(step2[:8], step1[:8])
	step2[8] = dctConstRoundShift(int64(step1[8])*cospi30_64 - int64(step1[15])*cospi2_64)
	step2[15] = dctConstRoundShift(int64(step1[8])*cospi2_64 + int64(step1[15])*cospi30_64)
	step2[9] = dctConstRoundShift(int64(step1[9])*cospi14_64 - int64(step1[14])*cospi18_64)
	step2[14] = dctConstRoundShift(int64(step1[9])*cospi18_64 + int64(step1[14])*cospi14_64)
	step2[10] = dctConstRoundShift(int64(step1[10])*cospi22_64 - int64(step1[13])*cospi10_64)
	step2[13] = dctConstRoundShift(int64(step1[10])*cospi10_64 + int64(step1[13])*cospi22_64)
	step2[11] = dctConstRoundShift(int64(step1[11])*cospi6_64 - int64(step1[12])*cospi26_64)
	step2[12] = dctConstRoundShift(int64(step1[11])*cospi26_64 + int64(step1[12])*cospi6_64)
	for i := 16; i < 32; i += 4 {
		step2[i] = step1[i] + step1[i+1]
		step2[i+1] = step1[i] - step1[i+1]
		step2[i+2] = -step1[i+2] + step1[i+3]
		step2[i+3] = step1[i+2] + step1[i+3]
	}

	copy(step1[:4], step2[:4])
	step1[4] = dctConstRoundShift(int64(step2[4])*cospi28_64 - int64(step2[7])*cospi4_64)
	step1[7] = dctConstRoundShift(int64(step2[4])*cospi4_64 + int64(step2[7])*cospi28_64)
	step1[5] = dctConstRoundShift(int64(step2[5])*cospi12_64 - int64(step2[6])*cospi20_64)
	step1[6] = dctConstRoundShift(int64(step2[5])*cospi20_64 + int64(step2[6])*cospi12_64)
	for i := 8; i < 16; i += 4 {
		step1[i] = step2[i] + step2[i+1]
		step1[i+1] = step2[i] - step2[i+1]
		step1[i+2] = -step2[i+2] + step2[i+3]
		step1[i+3] = step2[i+2] + step2[i+3]
	}
	step1[16] = step2[16]
	step1[31] = step2[31]
	step1[17] = dctConstRoundShift(-int64(step2[17])*cospi4_64 + int64(step2[30])*cospi28_64)
	step1[30] = dctConstRoundShift(int64(step2[17])*cospi28_64 + int64(step2[30])*cospi4_64)
	step1[18] = dctConstRoundShift(-int64(step2[18])*cospi28_64 - int64(step2[29])*cospi4_64)
	step1[29] = dctConstRoundShift(-int64(step2[18])*cospi4_64 + int64(step2[29])*cospi28_64)
	step1[19] = step2[19]
	step1[20] = step2[20]
	step1[21] = dctConstRoundShift(-int64(step2[21])*cospi20_64 + int64(step2[26])*cospi12_64)
	step1[26] = dctConstRoundShift(int64(step2[21])*cospi12_64 + int64(step2[26])*cospi20_64)
	step1[22] = dctConstRoundShift(-int64(step2[22])*cospi12_64 - int64(step2[25])*cospi20_64)
	step1[25] = dctConstRoundShift(-int64(step2[22])*cospi20_64 + int64(step2[25])*cospi12_64)
	step1[23] = step2[23]
	step1[24] = step2[24]
	step1[27] = step2[27]
	step1[28] = step2[28]

	step2[0] = dctConstRoundShift(int64(step1[0]+step1[1]) * cospi16_64)
	step2[1] = dctConstRoundShift(int64(step1[0]-step1[1]) * cospi16_64)
	step2[2] = dctConstRoundShift(int64(step1[2])*cospi24_64 - int64(step1[3])*cospi8_64)
	step2[3] = dctConstRoundShift(int64(step1[2])*cospi8_64 + int64(step1[3])*cospi24_64)
	step2[4] = step1[4] + step1[5]
	step2[5] = step1[4] - step1[5]
	step2[6] = -step1[6] + step1[7]
	step2[7] = step1[6] + step1[7]
	step2[8] = step1[8]
	step2[15] = step1[15]
	step2[9] = dctConstRoundShift(-int64(step1[9])*cospi8_64 + int64(step1[14])*cospi24_64)
	step2[14] = dctConstRoundShift(int64(step1[9])*cospi24_64 + int64(step1[14])*cospi8_64)
	step2[10] = dctConstRoundShift(-int64(step1[10])*cospi24_64 - int64(step1[13])*cospi8_64)
	step2[13] = dctConstRoundShift(-int64(step1[10])*cospi8_64 + int64(step1[13])*cospi24_64)
	step2[11] = step1[11]
	step2[12] = step1[12]
	for base := 16; base <= 24; base += 8 {
		step2[base+0] = step1[base+0] + step1[base+3]
		step2[base+1] = step1[base+1] + step1[base+2]
		step2[base+2] = step1[base+1] - step1[base+2]
		step2[base+3] = step1[base+0] - step1[base+3]
		step2[base+4] = -step1[base+4] + step1[base+7]
		step2[base+5] = -step1[base+5] + step1[base+6]
		step2[base+6] = step1[base+5] + step1[base+6]
		step2[base+7] = step1[base+4] + step1[base+7]
	}

	step1[0] = step2[0] + step2[3]
	step1[1] = step2[1] + step2[2]
	step1[2] = step2[1] - step2[2]
	step1[3] = step2[0] - step2[3]
	step1[4] = step2[4]
	step1[5] = dctConstRoundShift(int64(step2[6]-step2[5]) * cospi16_64)
	step1[6] = dctConstRoundShift(int64(step2[5]+step2[6]) * cospi16_64)
	step1[7] = step2[7]
	step1[8] = step2[8] + step2[11]
	step1[9] = step2[9] + step2[10]
	step1[10] = step2[9] - step2[10]
	step1[11] = step2[8] - step2[11]
	step1[12] = -step2[12] + step2[15]
	step1[13] = -step2[13] + step2[14]
	step1[14] = step2[13] + step2[14]
	step1[15] = step2[12] + step2[15]
	step1[16] = step2[16]
	step1[17] = step2[17]
	step1[18] = dctConstRoundShift(-int64(step2[18])*cospi8_64 + int64(step2[29])*cospi24_64)
	step1[29] = dctConstRoundShift(int64(step2[18])*cospi24_64 + int64(step2[29])*cospi8_64)
	step1[19] = dctConstRoundShift(-int64(step2[19])*cospi8_64 + int64(step2[28])*cospi24_64)
	step1[28] = dctConstRoundShift(int64(step2[19])*cospi24_64 + int64(step2[28])*cospi8_64)
	step1[20] = dctConstRoundShift(-int64(step2[20])*cospi24_64 - int64(step2[27])*cospi8_64)
	step1[27] = dctConstRoundShift(-int64(step2[20])*cospi8_64 + int64(step2[27])*cospi24_64)
	step1[21] = dctConstRoundShift(-int64(step2[21])*cospi24_64 - int64(step2[26])*cospi8_64)
	step1[26] = dctConstRoundShift(-int64(step2[21])*cospi8_64 + int64(step2[26])*cospi24_64)
	step1[22] = step2[22]
	step1[23] = step2[23]
	step1[24] = step2[24]
	step1[25] = step2[25]
	step1[30] = step2[30]
	step1[31] = step2[31]

	for i := range 4 {
		step2[i] = step1[i] + step1[7-i]
		step2[7-i] = step1[i] - step1[7-i]
	}
	step2[8] = step1[8]
	step2[9] = step1[9]
	step2[10] = dctConstRoundShift(int64(-step1[10]+step1[13]) * cospi16_64)
	step2[13] = dctConstRoundShift(int64(step1[10]+step1[13]) * cospi16_64)
	step2[11] = dctConstRoundShift(int64(-step1[11]+step1[12]) * cospi16_64)
	step2[12] = dctConstRoundShift(int64(step1[11]+step1[12]) * cospi16_64)
	step2[14] = step1[14]
	step2[15] = step1[15]
	for i := range 4 {
		step2[16+i] = step1[16+i] + step1[23-i]
		step2[23-i] = step1[16+i] - step1[23-i]
		step2[24+i] = -step1[24+i] + step1[31-i]
		step2[31-i] = step1[24+i] + step1[31-i]
	}

	for i := range 8 {
		step1[i] = step2[i] + step2[15-i]
		step1[15-i] = step2[i] - step2[15-i]
	}
	copy(step1[16:20], step2[16:20])
	for i := range 4 {
		step1[20+i] = dctConstRoundShift(int64(-step2[20+i]+step2[27-i]) * cospi16_64)
		step1[27-i] = dctConstRoundShift(int64(step2[20+i]+step2[27-i]) * cospi16_64)
	}
	copy(step1[28:32], step2[28:32])

	for i := range 16 {
		output[i] = step1[i] + step1[31-i]
		output[31-i] = step1[i] - step1[31-i]
	}
}

// HighbdInverseTransformAdd applies the VP9 inverse transform (4x4, 8x8, 16x16,
// 32x32, DCT/ADST/WHT) on int32 coefficients and adds the residual into uint16
// dest, clamping to [0, (1<<bd)-1].
func HighbdInverseTransformAdd(txSize common.TxSize, txType common.TxType,
	lossless bool, input []int32, dest []uint16, dstOff, stride, eob, bd int,
) {
	if eob <= 0 {
		return
	}
	if lossless {
		highbdIwht4x4Add(input, dest, dstOff, stride, eob, bd)
		return
	}
	switch txSize {
	case common.Tx4x4:
		if txType == common.DctDct && eob == 1 {
			out := dctConstRoundShift(int64(input[0]) * cospi16_64)
			out = dctConstRoundShift(int64(out) * cospi16_64)
			a1 := highbdRoundShift(out, 4)
			for r := range 4 {
				row := dstOff + r*stride
				dest[row+0] = highbdClipPixelAdd(dest[row+0], a1, bd)
				dest[row+1] = highbdClipPixelAdd(dest[row+1], a1, bd)
				dest[row+2] = highbdClipPixelAdd(dest[row+2], a1, bd)
				dest[row+3] = highbdClipPixelAdd(dest[row+3], a1, bd)
			}
			return
		}
		var out [16]int32
		var colIn, colOut [4]int32
		rowAdst := txType == common.DctAdst || txType == common.AdstAdst
		colAdst := txType == common.AdstDct || txType == common.AdstAdst
		for r := range 4 {
			if rowAdst {
				highbdIadst4(input[r*4:], out[r*4:])
			} else {
				highbdIdct4(input[r*4:], out[r*4:])
			}
		}
		for c := range 4 {
			for r := range 4 {
				colIn[r] = out[r*4+c]
			}
			if colAdst {
				highbdIadst4(colIn[:], colOut[:])
			} else {
				highbdIdct4(colIn[:], colOut[:])
			}
			for r := range 4 {
				dest[dstOff+r*stride+c] = highbdClipPixelAdd(
					dest[dstOff+r*stride+c], highbdRoundShift(colOut[r], 4), bd)
			}
		}
	case common.Tx8x8:
		if txType == common.DctDct && eob == 1 {
			out := dctConstRoundShift(int64(input[0]) * cospi16_64)
			out = dctConstRoundShift(int64(out) * cospi16_64)
			a1 := highbdRoundShift(out, 5)
			for r := range 8 {
				row := dstOff + r*stride
				for c := range 8 {
					dest[row+c] = highbdClipPixelAdd(dest[row+c], a1, bd)
				}
			}
			return
		}
		var out [64]int32
		var colIn, colOut [8]int32
		rowAdst := txType == common.DctAdst || txType == common.AdstAdst
		colAdst := txType == common.AdstDct || txType == common.AdstAdst
		for r := range 8 {
			if rowAdst {
				highbdIadst8(input[r*8:], out[r*8:])
			} else {
				highbdIdct8(input[r*8:], out[r*8:])
			}
		}
		for c := range 8 {
			for r := range 8 {
				colIn[r] = out[r*8+c]
			}
			if colAdst {
				highbdIadst8(colIn[:], colOut[:])
			} else {
				highbdIdct8(colIn[:], colOut[:])
			}
			for r := range 8 {
				dest[dstOff+r*stride+c] = highbdClipPixelAdd(
					dest[dstOff+r*stride+c], highbdRoundShift(colOut[r], 5), bd)
			}
		}
	case common.Tx16x16:
		if txType == common.DctDct && eob == 1 {
			out := dctConstRoundShift(int64(input[0]) * cospi16_64)
			out = dctConstRoundShift(int64(out) * cospi16_64)
			a1 := highbdRoundShift(out, 6)
			for r := range 16 {
				row := dstOff + r*stride
				for c := range 16 {
					dest[row+c] = highbdClipPixelAdd(dest[row+c], a1, bd)
				}
			}
			return
		}
		var out [256]int32
		var colIn, colOut [16]int32
		rowAdst := txType == common.DctAdst || txType == common.AdstAdst
		colAdst := txType == common.AdstDct || txType == common.AdstAdst
		for r := range 16 {
			if rowAdst {
				highbdIadst16(input[r*16:], out[r*16:])
			} else {
				highbdIdct16(input[r*16:], out[r*16:])
			}
		}
		for c := range 16 {
			for r := range 16 {
				colIn[r] = out[r*16+c]
			}
			if colAdst {
				highbdIadst16(colIn[:], colOut[:])
			} else {
				highbdIdct16(colIn[:], colOut[:])
			}
			for r := range 16 {
				dest[dstOff+r*stride+c] = highbdClipPixelAdd(
					dest[dstOff+r*stride+c], highbdRoundShift(colOut[r], 6), bd)
			}
		}
	case common.Tx32x32:
		if eob == 1 {
			out := dctConstRoundShift(int64(input[0]) * cospi16_64)
			out = dctConstRoundShift(int64(out) * cospi16_64)
			a1 := highbdRoundShift(out, 6)
			for r := range 32 {
				row := dstOff + r*stride
				for c := range 32 {
					dest[row+c] = highbdClipPixelAdd(dest[row+c], a1, bd)
				}
			}
			return
		}
		var out [1024]int32
		var colIn, colOut [32]int32
		for r := range 32 {
			in := input[r*32 : r*32+32]
			var nz int32
			for j := range 32 {
				nz |= in[j]
			}
			if nz != 0 {
				highbdIdct32(in, out[r*32:])
			}
		}
		for c := range 32 {
			for r := range 32 {
				colIn[r] = out[r*32+c]
			}
			highbdIdct32(colIn[:], colOut[:])
			for r := range 32 {
				dest[dstOff+r*stride+c] = highbdClipPixelAdd(
					dest[dstOff+r*stride+c], highbdRoundShift(colOut[r], 6), bd)
			}
		}
	}
}

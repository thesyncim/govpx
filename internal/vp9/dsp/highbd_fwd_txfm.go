package dsp

import "github.com/thesyncim/govpx/internal/vp9/common"

// High-bit-depth (10-bit / 12-bit) VP9 forward transforms.
// Ported verbatim from libvpx v1.16.0 vpx_dsp/fwd_txfm.c and vp9/encoder/vp9_dct.c.

const highbdUnitQuantFactor = 4

func fdctRoundShift(input int64) int32 {
	if input < 0 {
		return int32((input + dctConstRounding - 1) >> dctConstBits)
	}
	return int32((input + dctConstRounding) >> dctConstBits)
}

// HighbdFwht4x4 computes the lossless 4x4 forward Walsh-Hadamard transform
// into int32 output (scaled by UNIT_QUANT_FACTOR = 4).
func HighbdFwht4x4(input []int16, output []int32, stride int) {
	for i := range 4 {
		a1 := int64(input[0*stride+i])
		b1 := int64(input[1*stride+i])
		c1 := int64(input[2*stride+i])
		d1 := int64(input[3*stride+i])

		a1 += b1
		d1 -= c1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= c1
		d1 += b1

		output[0+i] = int32(a1)
		output[4+i] = int32(c1)
		output[8+i] = int32(d1)
		output[12+i] = int32(b1)
	}
	for i := range 4 {
		base := i * 4
		a1 := int64(output[base+0])
		b1 := int64(output[base+1])
		c1 := int64(output[base+2])
		d1 := int64(output[base+3])

		a1 += b1
		d1 -= c1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= c1
		d1 += b1

		output[base+0] = int32(a1 * highbdUnitQuantFactor)
		output[base+1] = int32(c1 * highbdUnitQuantFactor)
		output[base+2] = int32(d1 * highbdUnitQuantFactor)
		output[base+3] = int32(b1 * highbdUnitQuantFactor)
	}
}

// HighbdFdct4x4 computes the 4x4 forward DCT into int32 output.
func HighbdFdct4x4(input []int16, output []int32, stride int) {
	var intermediate [16]int32
	for pass := range 2 {
		out := intermediate[:]
		if pass == 1 {
			out = output
		}
		for i := range 4 {
			var in0, in1, in2, in3 int64
			if pass == 0 {
				in0 = int64(input[0*stride+i]) * 16
				in1 = int64(input[1*stride+i]) * 16
				in2 = int64(input[2*stride+i]) * 16
				in3 = int64(input[3*stride+i]) * 16
				if i == 0 && in0 != 0 {
					in0++
				}
			} else {
				in0 = int64(intermediate[0*4+i])
				in1 = int64(intermediate[1*4+i])
				in2 = int64(intermediate[2*4+i])
				in3 = int64(intermediate[3*4+i])
			}
			s0 := in0 + in3
			s1 := in1 + in2
			s2 := in1 - in2
			s3 := in0 - in3
			out[i*4+0] = fdctRoundShift((s0 + s1) * cospi16_64)
			out[i*4+2] = fdctRoundShift((s0 - s1) * cospi16_64)
			out[i*4+1] = fdctRoundShift(s2*cospi24_64 + s3*cospi8_64)
			out[i*4+3] = fdctRoundShift(-s2*cospi8_64 + s3*cospi24_64)
		}
	}
	for i := range 16 {
		output[i] = (output[i] + 1) >> 2
	}
}

// HighbdFdct8x8 computes the 8x8 forward DCT into int32 output.
func HighbdFdct8x8(input []int16, output []int32, stride int) {
	var intermediate [64]int32
	for pass := range 2 {
		out := intermediate[:]
		if pass == 1 {
			out = output
		}
		for i := range 8 {
			var s0, s1, s2, s3, s4, s5, s6, s7 int64
			if pass == 0 {
				s0 = int64(input[0*stride+i]+input[7*stride+i]) * 4
				s1 = int64(input[1*stride+i]+input[6*stride+i]) * 4
				s2 = int64(input[2*stride+i]+input[5*stride+i]) * 4
				s3 = int64(input[3*stride+i]+input[4*stride+i]) * 4
				s4 = int64(input[3*stride+i]-input[4*stride+i]) * 4
				s5 = int64(input[2*stride+i]-input[5*stride+i]) * 4
				s6 = int64(input[1*stride+i]-input[6*stride+i]) * 4
				s7 = int64(input[0*stride+i]-input[7*stride+i]) * 4
			} else {
				s0 = int64(intermediate[0*8+i]) + int64(intermediate[7*8+i])
				s1 = int64(intermediate[1*8+i]) + int64(intermediate[6*8+i])
				s2 = int64(intermediate[2*8+i]) + int64(intermediate[5*8+i])
				s3 = int64(intermediate[3*8+i]) + int64(intermediate[4*8+i])
				s4 = int64(intermediate[3*8+i]) - int64(intermediate[4*8+i])
				s5 = int64(intermediate[2*8+i]) - int64(intermediate[5*8+i])
				s6 = int64(intermediate[1*8+i]) - int64(intermediate[6*8+i])
				s7 = int64(intermediate[0*8+i]) - int64(intermediate[7*8+i])
			}
			x0 := s0 + s3
			x1 := s1 + s2
			x2 := s1 - s2
			x3 := s0 - s3
			out[i*8+0] = fdctRoundShift((x0 + x1) * cospi16_64)
			out[i*8+2] = fdctRoundShift(x2*cospi24_64 + x3*cospi8_64)
			out[i*8+4] = fdctRoundShift((x0 - x1) * cospi16_64)
			out[i*8+6] = fdctRoundShift(-x2*cospi8_64 + x3*cospi24_64)

			t2 := int64(fdctRoundShift((s6 - s5) * cospi16_64))
			t3 := int64(fdctRoundShift((s6 + s5) * cospi16_64))
			x0 = s4 + t2
			x1 = s4 - t2
			x2 = s7 - t3
			x3 = s7 + t3
			out[i*8+1] = fdctRoundShift(x0*cospi28_64 + x3*cospi4_64)
			out[i*8+3] = fdctRoundShift(x2*cospi12_64 - x1*cospi20_64)
			out[i*8+5] = fdctRoundShift(x1*cospi12_64 + x2*cospi20_64)
			out[i*8+7] = fdctRoundShift(x3*cospi28_64 - x0*cospi4_64)
		}
	}
	for i := range 64 {
		output[i] /= 2
	}
}

func highbdFadst4_1d(in [4]int64) [4]int64 {
	x0, x1, x2, x3 := in[0], in[1], in[2], in[3]
	if x0|x1|x2|x3 == 0 {
		return [4]int64{}
	}
	s0 := sinpi1_9 * x0
	s1 := sinpi4_9 * x0
	s2 := sinpi2_9 * x1
	s3 := sinpi1_9 * x1
	s4 := sinpi3_9 * x2
	s5 := sinpi4_9 * x3
	s6 := sinpi2_9 * x3
	s7 := x0 + x1 - x3

	x0 = s0 + s2 + s5
	x1 = sinpi3_9 * s7
	x2 = s1 - s3 + s6
	x3 = s4

	return [4]int64{
		int64(fdctRoundShift(x0 + x3)),
		int64(fdctRoundShift(x1)),
		int64(fdctRoundShift(x2 - x3)),
		int64(fdctRoundShift(x2 - x0 + x3)),
	}
}

func highbdFdct4_1d(in [4]int64) [4]int64 {
	s0 := in[0] + in[3]
	s1 := in[1] + in[2]
	s2 := in[1] - in[2]
	s3 := in[0] - in[3]
	return [4]int64{
		int64(fdctRoundShift((s0 + s1) * cospi16_64)),
		int64(fdctRoundShift(s2*cospi24_64 + s3*cospi8_64)),
		int64(fdctRoundShift((s0 - s1) * cospi16_64)),
		int64(fdctRoundShift(-s2*cospi8_64 + s3*cospi24_64)),
	}
}

// HighbdFht4x4 computes the 4x4 hybrid forward transform (DCT/ADST) into int32.
func HighbdFht4x4(input []int16, output []int32, stride int, txType common.TxType) {
	if txType == common.DctDct {
		HighbdFdct4x4(input, output, stride)
		return
	}
	var out [16]int64
	for i := range 4 {
		in := [4]int64{
			int64(input[0*stride+i]) * 16,
			int64(input[1*stride+i]) * 16,
			int64(input[2*stride+i]) * 16,
			int64(input[3*stride+i]) * 16,
		}
		if i == 0 && in[0] != 0 {
			in[0]++
		}
		var tmp [4]int64
		if txType == common.AdstDct || txType == common.AdstAdst {
			tmp = highbdFadst4_1d(in)
		} else {
			tmp = highbdFdct4_1d(in)
		}
		for j := range 4 {
			out[j*4+i] = tmp[j]
		}
	}
	for i := range 4 {
		in := [4]int64{out[i*4+0], out[i*4+1], out[i*4+2], out[i*4+3]}
		var tmp [4]int64
		if txType == common.DctAdst || txType == common.AdstAdst {
			tmp = highbdFadst4_1d(in)
		} else {
			tmp = highbdFdct4_1d(in)
		}
		for j := range 4 {
			output[i*4+j] = int32((tmp[j] + 1) >> 2)
		}
	}
}

func highbdFdct8_1d(in [8]int64) [8]int64 {
	s0 := in[0] + in[7]
	s1 := in[1] + in[6]
	s2 := in[2] + in[5]
	s3 := in[3] + in[4]
	s4 := in[3] - in[4]
	s5 := in[2] - in[5]
	s6 := in[1] - in[6]
	s7 := in[0] - in[7]

	x0 := s0 + s3
	x1 := s1 + s2
	x2 := s1 - s2
	x3 := s0 - s3
	var out [8]int64
	out[0] = int64(fdctRoundShift((x0 + x1) * cospi16_64))
	out[2] = int64(fdctRoundShift(x2*cospi24_64 + x3*cospi8_64))
	out[4] = int64(fdctRoundShift((x0 - x1) * cospi16_64))
	out[6] = int64(fdctRoundShift(-x2*cospi8_64 + x3*cospi24_64))

	t2 := int64(fdctRoundShift((s6 - s5) * cospi16_64))
	t3 := int64(fdctRoundShift((s6 + s5) * cospi16_64))
	x0 = s4 + t2
	x1 = s4 - t2
	x2 = s7 - t3
	x3 = s7 + t3
	out[1] = int64(fdctRoundShift(x0*cospi28_64 + x3*cospi4_64))
	out[3] = int64(fdctRoundShift(x2*cospi12_64 - x1*cospi20_64))
	out[5] = int64(fdctRoundShift(x1*cospi12_64 + x2*cospi20_64))
	out[7] = int64(fdctRoundShift(x3*cospi28_64 - x0*cospi4_64))
	return out
}

func highbdFadst8_1d(in [8]int64) [8]int64 {
	x0, x1, x2, x3 := in[7], in[0], in[5], in[2]
	x4, x5, x6, x7 := in[3], in[4], in[1], in[6]

	s0 := cospi2_64*x0 + cospi30_64*x1
	s1 := cospi30_64*x0 - cospi2_64*x1
	s2 := cospi10_64*x2 + cospi22_64*x3
	s3 := cospi22_64*x2 - cospi10_64*x3
	s4 := cospi18_64*x4 + cospi14_64*x5
	s5 := cospi14_64*x4 - cospi18_64*x5
	s6 := cospi26_64*x6 + cospi6_64*x7
	s7 := cospi6_64*x6 - cospi26_64*x7

	x0 = int64(fdctRoundShift(s0 + s4))
	x1 = int64(fdctRoundShift(s1 + s5))
	x2 = int64(fdctRoundShift(s2 + s6))
	x3 = int64(fdctRoundShift(s3 + s7))
	x4 = int64(fdctRoundShift(s0 - s4))
	x5 = int64(fdctRoundShift(s1 - s5))
	x6 = int64(fdctRoundShift(s2 - s6))
	x7 = int64(fdctRoundShift(s3 - s7))

	s0, s1, s2, s3 = x0, x1, x2, x3
	s4 = cospi8_64*x4 + cospi24_64*x5
	s5 = cospi24_64*x4 - cospi8_64*x5
	s6 = -cospi24_64*x6 + cospi8_64*x7
	s7 = cospi8_64*x6 + cospi24_64*x7

	x0 = s0 + s2
	x1 = s1 + s3
	x2 = s0 - s2
	x3 = s1 - s3
	x4 = int64(fdctRoundShift(s4 + s6))
	x5 = int64(fdctRoundShift(s5 + s7))
	x6 = int64(fdctRoundShift(s4 - s6))
	x7 = int64(fdctRoundShift(s5 - s7))

	x2, x3 = int64(fdctRoundShift(cospi16_64*(x2+x3))), int64(fdctRoundShift(cospi16_64*(x2-x3)))
	x6, x7 = int64(fdctRoundShift(cospi16_64*(x6+x7))), int64(fdctRoundShift(cospi16_64*(x6-x7)))

	return [8]int64{x0, -x4, x6, -x2, x3, -x7, x5, -x1}
}

// HighbdFht8x8 computes the 8x8 hybrid forward transform (DCT/ADST) into int32.
func HighbdFht8x8(input []int16, output []int32, stride int, txType common.TxType) {
	if txType == common.DctDct {
		HighbdFdct8x8(input, output, stride)
		return
	}
	var out [64]int64
	for i := range 8 {
		var in [8]int64
		for j := range 8 {
			in[j] = int64(input[j*stride+i]) * 4
		}
		var tmp [8]int64
		if txType == common.AdstDct || txType == common.AdstAdst {
			tmp = highbdFadst8_1d(in)
		} else {
			tmp = highbdFdct8_1d(in)
		}
		for j := range 8 {
			out[j*8+i] = tmp[j]
		}
	}
	for i := range 8 {
		var in [8]int64
		for j := range 8 {
			in[j] = out[i*8+j]
		}
		var tmp [8]int64
		if txType == common.DctAdst || txType == common.AdstAdst {
			tmp = highbdFadst8_1d(in)
		} else {
			tmp = highbdFdct8_1d(in)
		}
		for j := range 8 {
			v := tmp[j]
			if v < 0 {
				v++
			}
			output[i*8+j] = int32(v >> 1)
		}
	}
}

// HighbdFdct16x16 computes the 16x16 forward DCT into int32 output.
func HighbdFdct16x16(input []int16, output []int32, stride int) {
	var intermediate [256]int64
	for i := range 16 {
		var in [16]int64
		for j := range 16 {
			in[j] = int64(input[j*stride+i]) * 4
		}
		tmp := highbdFdct16_1d(in)
		for j := range 16 {
			intermediate[i*16+j] = tmp[j]
		}
	}
	for i := range 16 {
		var in [16]int64
		for j := range 16 {
			v := intermediate[j*16+i]
			if v < 0 {
				in[j] = (v + 2) >> 2
			} else {
				in[j] = (v + 1) >> 2
			}
		}
		tmp := highbdFdct16_1d(in)
		for j := range 16 {
			output[i*16+j] = int32(tmp[j])
		}
	}
}

func highbdFdct16_1d(in [16]int64) [16]int64 {
	var even [8]int64
	var step1 [8]int64
	for i := range 8 {
		even[i] = in[i] + in[15-i]
		step1[i] = in[7-i] - in[8+i]
	}
	dct8 := highbdFdct8_1d(even)
	var out [16]int64
	out[0], out[4], out[8], out[12] = dct8[0], dct8[2], dct8[4], dct8[6]
	out[2], out[6], out[10], out[14] = dct8[1], dct8[3], dct8[5], dct8[7]

	s2_2 := int64(fdctRoundShift((step1[5] - step1[2]) * cospi16_64))
	s2_3 := int64(fdctRoundShift((step1[4] - step1[3]) * cospi16_64))
	s2_4 := int64(fdctRoundShift((step1[4] + step1[3]) * cospi16_64))
	s2_5 := int64(fdctRoundShift((step1[5] + step1[2]) * cospi16_64))

	s3_0 := step1[0] + s2_3
	s3_1 := step1[1] + s2_2
	s3_2 := step1[1] - s2_2
	s3_3 := step1[0] - s2_3
	s3_4 := step1[7] - s2_4
	s3_5 := step1[6] - s2_5
	s3_6 := step1[6] + s2_5
	s3_7 := step1[7] + s2_4

	s2_1 := int64(fdctRoundShift(-s3_1*cospi8_64 + s3_6*cospi24_64))
	s2_2 = int64(fdctRoundShift(s3_2*cospi24_64 + s3_5*cospi8_64))
	s2_5 = int64(fdctRoundShift(s3_2*cospi8_64 - s3_5*cospi24_64))
	s2_6 := int64(fdctRoundShift(s3_1*cospi24_64 + s3_6*cospi8_64))

	step1[0] = s3_0 + s2_1
	step1[1] = s3_0 - s2_1
	step1[2] = s3_3 + s2_2
	step1[3] = s3_3 - s2_2
	step1[4] = s3_4 - s2_5
	step1[5] = s3_4 + s2_5
	step1[6] = s3_7 - s2_6
	step1[7] = s3_7 + s2_6

	out[1] = int64(fdctRoundShift(step1[0]*cospi30_64 + step1[7]*cospi2_64))
	out[9] = int64(fdctRoundShift(step1[1]*cospi14_64 + step1[6]*cospi18_64))
	out[5] = int64(fdctRoundShift(step1[2]*cospi22_64 + step1[5]*cospi10_64))
	out[13] = int64(fdctRoundShift(step1[3]*cospi6_64 + step1[4]*cospi26_64))
	out[3] = int64(fdctRoundShift(-step1[3]*cospi26_64 + step1[4]*cospi6_64))
	out[11] = int64(fdctRoundShift(-step1[2]*cospi10_64 + step1[5]*cospi22_64))
	out[7] = int64(fdctRoundShift(-step1[1]*cospi18_64 + step1[6]*cospi14_64))
	out[15] = int64(fdctRoundShift(-step1[0]*cospi2_64 + step1[7]*cospi30_64))
	return out
}

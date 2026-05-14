package encoder

import "github.com/thesyncim/govpx/internal/vp9/common"

const (
	fdctDctConstBits     = 14
	fdctDctConstRounding = 1 << (fdctDctConstBits - 1)

	fdctCospi1_64  = 16364
	fdctCospi2_64  = 16305
	fdctCospi3_64  = 16207
	fdctCospi4_64  = 16069
	fdctCospi5_64  = 15893
	fdctCospi6_64  = 15679
	fdctCospi7_64  = 15426
	fdctCospi8_64  = 15137
	fdctCospi9_64  = 14811
	fdctCospi10_64 = 14449
	fdctCospi11_64 = 14053
	fdctCospi12_64 = 13623
	fdctCospi13_64 = 13160
	fdctCospi14_64 = 12665
	fdctCospi15_64 = 12140
	fdctCospi16_64 = 11585
	fdctCospi17_64 = 11003
	fdctCospi18_64 = 10394
	fdctCospi19_64 = 9760
	fdctCospi20_64 = 9102
	fdctCospi21_64 = 8423
	fdctCospi22_64 = 7723
	fdctCospi23_64 = 7005
	fdctCospi24_64 = 6270
	fdctCospi25_64 = 5520
	fdctCospi26_64 = 4756
	fdctCospi27_64 = 3981
	fdctCospi28_64 = 3196
	fdctCospi29_64 = 2404
	fdctCospi30_64 = 1606
	fdctCospi31_64 = 804

	fdctSinpi1_9 = 5283
	fdctSinpi2_9 = 9929
	fdctSinpi3_9 = 13377
	fdctSinpi4_9 = 15212
)

// ForwardDCT4x4 mirrors libvpx v1.16.0 vpx_fdct4x4_c. Input is a 4x4
// residual block with caller-provided stride; output is raster-order
// transform coefficients.
func ForwardDCT4x4(input []int16, stride int, output *[16]int16) {
	ForwardDCT4x4Into(input, stride, output[:])
}

// ForwardDCT4x4Into is the slice-backed form of ForwardDCT4x4. output must
// hold at least 16 coefficients.
func ForwardDCT4x4Into(input []int16, stride int, output []int16) {
	var intermediate [16]int
	var final [16]int

	for pass := 0; pass < 2; pass++ {
		out := intermediate[:]
		if pass == 1 {
			out = final[:]
		}
		for i := 0; i < 4; i++ {
			var in0, in1, in2, in3 int
			if pass == 0 {
				in0 = int(input[0*stride+i]) * 16
				in1 = int(input[1*stride+i]) * 16
				in2 = int(input[2*stride+i]) * 16
				in3 = int(input[3*stride+i]) * 16
				if i == 0 && in0 != 0 {
					in0++
				}
			} else {
				in0 = intermediate[0*4+i]
				in1 = intermediate[1*4+i]
				in2 = intermediate[2*4+i]
				in3 = intermediate[3*4+i]
			}

			step0 := in0 + in3
			step1 := in1 + in2
			step2 := in1 - in2
			step3 := in0 - in3
			out[0] = fdctRoundShift((step0 + step1) * fdctCospi16_64)
			out[2] = fdctRoundShift((step0 - step1) * fdctCospi16_64)
			out[1] = fdctRoundShift(step2*fdctCospi24_64 + step3*fdctCospi8_64)
			out[3] = fdctRoundShift(-step2*fdctCospi8_64 + step3*fdctCospi24_64)

			out = out[4:]
		}
	}

	for i := range 16 {
		output[i] = int16((final[i] + 1) >> 2)
	}
}

// ForwardWHT4x4Into mirrors libvpx v1.16.0 vp9_fwht4x4_c. VP9 lossless
// mode uses this reversible 4x4 Walsh-Hadamard transform instead of the
// normal DCT path.
func ForwardWHT4x4Into(input []int16, stride int, output []int16) {
	var tmp [16]int

	for i := 0; i < 4; i++ {
		a1 := int(input[0*stride+i])
		b1 := int(input[1*stride+i])
		c1 := int(input[2*stride+i])
		d1 := int(input[3*stride+i])

		a1 += b1
		d1 -= c1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= c1
		d1 += b1

		tmp[0*4+i] = a1
		tmp[1*4+i] = c1
		tmp[2*4+i] = d1
		tmp[3*4+i] = b1
	}

	n := len(output)
	if n > 16 {
		n = 16
	}
	for i := range n {
		output[i] = 0
	}
	if len(output) < 16 {
		return
	}

	for i := 0; i < 4; i++ {
		base := i * 4
		a1 := tmp[base+0]
		b1 := tmp[base+1]
		c1 := tmp[base+2]
		d1 := tmp[base+3]

		a1 += b1
		d1 -= c1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= c1
		d1 += b1

		output[base+0] = int16(a1 << 2)
		output[base+1] = int16(c1 << 2)
		output[base+2] = int16(d1 << 2)
		output[base+3] = int16(b1 << 2)
	}
}

// ForwardDCT8x8 mirrors libvpx v1.16.0 vpx_fdct8x8_c. Input is an 8x8
// residual block with caller-provided stride; output is raster-order
// transform coefficients.
func ForwardDCT8x8(input []int16, stride int, output *[64]int16) {
	ForwardDCT8x8Into(input, stride, output[:])
}

// ForwardDCT8x8Into is the slice-backed form of ForwardDCT8x8. output must
// hold at least 64 coefficients.
func ForwardDCT8x8Into(input []int16, stride int, output []int16) {
	var intermediate [64]int
	var final [64]int

	for pass := 0; pass < 2; pass++ {
		for i := 0; i < 8; i++ {
			var s0, s1, s2, s3, s4, s5, s6, s7 int
			if pass == 0 {
				s0 = (int(input[0*stride+i]) + int(input[7*stride+i])) * 4
				s1 = (int(input[1*stride+i]) + int(input[6*stride+i])) * 4
				s2 = (int(input[2*stride+i]) + int(input[5*stride+i])) * 4
				s3 = (int(input[3*stride+i]) + int(input[4*stride+i])) * 4
				s4 = (int(input[3*stride+i]) - int(input[4*stride+i])) * 4
				s5 = (int(input[2*stride+i]) - int(input[5*stride+i])) * 4
				s6 = (int(input[1*stride+i]) - int(input[6*stride+i])) * 4
				s7 = (int(input[0*stride+i]) - int(input[7*stride+i])) * 4
			} else {
				s0 = intermediate[0*8+i] + intermediate[7*8+i]
				s1 = intermediate[1*8+i] + intermediate[6*8+i]
				s2 = intermediate[2*8+i] + intermediate[5*8+i]
				s3 = intermediate[3*8+i] + intermediate[4*8+i]
				s4 = intermediate[3*8+i] - intermediate[4*8+i]
				s5 = intermediate[2*8+i] - intermediate[5*8+i]
				s6 = intermediate[1*8+i] - intermediate[6*8+i]
				s7 = intermediate[0*8+i] - intermediate[7*8+i]
			}

			x0 := s0 + s3
			x1 := s1 + s2
			x2 := s1 - s2
			x3 := s0 - s3

			base := i * 8
			out := intermediate[:]
			if pass == 1 {
				out = final[:]
			}
			out[base+0] = fdctRoundShift((x0 + x1) * fdctCospi16_64)
			out[base+2] = fdctRoundShift(x2*fdctCospi24_64 + x3*fdctCospi8_64)
			out[base+4] = fdctRoundShift((x0 - x1) * fdctCospi16_64)
			out[base+6] = fdctRoundShift(-x2*fdctCospi8_64 + x3*fdctCospi24_64)

			t0 := (s6 - s5) * fdctCospi16_64
			t1 := (s6 + s5) * fdctCospi16_64
			t2 := fdctRoundShift(t0)
			t3 := fdctRoundShift(t1)

			x0 = s4 + t2
			x1 = s4 - t2
			x2 = s7 - t3
			x3 = s7 + t3

			out[base+1] = fdctRoundShift(x0*fdctCospi28_64 + x3*fdctCospi4_64)
			out[base+3] = fdctRoundShift(x2*fdctCospi12_64 - x1*fdctCospi20_64)
			out[base+5] = fdctRoundShift(x1*fdctCospi12_64 + x2*fdctCospi20_64)
			out[base+7] = fdctRoundShift(x3*fdctCospi28_64 - x0*fdctCospi4_64)
		}
	}

	for i := range 64 {
		output[i] = int16(final[i] / 2)
	}
}

// ForwardDCT16x16 mirrors libvpx v1.16.0 vpx_fdct16x16_c. Input is a
// 16x16 residual block with caller-provided stride; output is raster-order
// transform coefficients.
func ForwardDCT16x16(input []int16, stride int, output *[256]int16) {
	ForwardDCT16x16Into(input, stride, output[:])
}

// ForwardDCT16x16Into is the slice-backed form of ForwardDCT16x16. output
// must hold at least 256 coefficients.
func ForwardDCT16x16Into(input []int16, stride int, output []int16) {
	var intermediate [256]int
	var final [256]int

	for pass := 0; pass < 2; pass++ {
		for i := 0; i < 16; i++ {
			var inHigh, step1, step2, step3 [8]int
			if pass == 0 {
				inHigh[0] = (int(input[0*stride+i]) + int(input[15*stride+i])) * 4
				inHigh[1] = (int(input[1*stride+i]) + int(input[14*stride+i])) * 4
				inHigh[2] = (int(input[2*stride+i]) + int(input[13*stride+i])) * 4
				inHigh[3] = (int(input[3*stride+i]) + int(input[12*stride+i])) * 4
				inHigh[4] = (int(input[4*stride+i]) + int(input[11*stride+i])) * 4
				inHigh[5] = (int(input[5*stride+i]) + int(input[10*stride+i])) * 4
				inHigh[6] = (int(input[6*stride+i]) + int(input[9*stride+i])) * 4
				inHigh[7] = (int(input[7*stride+i]) + int(input[8*stride+i])) * 4

				step1[0] = (int(input[7*stride+i]) - int(input[8*stride+i])) * 4
				step1[1] = (int(input[6*stride+i]) - int(input[9*stride+i])) * 4
				step1[2] = (int(input[5*stride+i]) - int(input[10*stride+i])) * 4
				step1[3] = (int(input[4*stride+i]) - int(input[11*stride+i])) * 4
				step1[4] = (int(input[3*stride+i]) - int(input[12*stride+i])) * 4
				step1[5] = (int(input[2*stride+i]) - int(input[13*stride+i])) * 4
				step1[6] = (int(input[1*stride+i]) - int(input[14*stride+i])) * 4
				step1[7] = (int(input[0*stride+i]) - int(input[15*stride+i])) * 4
			} else {
				inHigh[0] = fdctRoundShift2(intermediate[0*16+i]) + fdctRoundShift2(intermediate[15*16+i])
				inHigh[1] = fdctRoundShift2(intermediate[1*16+i]) + fdctRoundShift2(intermediate[14*16+i])
				inHigh[2] = fdctRoundShift2(intermediate[2*16+i]) + fdctRoundShift2(intermediate[13*16+i])
				inHigh[3] = fdctRoundShift2(intermediate[3*16+i]) + fdctRoundShift2(intermediate[12*16+i])
				inHigh[4] = fdctRoundShift2(intermediate[4*16+i]) + fdctRoundShift2(intermediate[11*16+i])
				inHigh[5] = fdctRoundShift2(intermediate[5*16+i]) + fdctRoundShift2(intermediate[10*16+i])
				inHigh[6] = fdctRoundShift2(intermediate[6*16+i]) + fdctRoundShift2(intermediate[9*16+i])
				inHigh[7] = fdctRoundShift2(intermediate[7*16+i]) + fdctRoundShift2(intermediate[8*16+i])

				step1[0] = fdctRoundShift2(intermediate[7*16+i]) - fdctRoundShift2(intermediate[8*16+i])
				step1[1] = fdctRoundShift2(intermediate[6*16+i]) - fdctRoundShift2(intermediate[9*16+i])
				step1[2] = fdctRoundShift2(intermediate[5*16+i]) - fdctRoundShift2(intermediate[10*16+i])
				step1[3] = fdctRoundShift2(intermediate[4*16+i]) - fdctRoundShift2(intermediate[11*16+i])
				step1[4] = fdctRoundShift2(intermediate[3*16+i]) - fdctRoundShift2(intermediate[12*16+i])
				step1[5] = fdctRoundShift2(intermediate[2*16+i]) - fdctRoundShift2(intermediate[13*16+i])
				step1[6] = fdctRoundShift2(intermediate[1*16+i]) - fdctRoundShift2(intermediate[14*16+i])
				step1[7] = fdctRoundShift2(intermediate[0*16+i]) - fdctRoundShift2(intermediate[15*16+i])
			}

			out := intermediate[:]
			if pass == 1 {
				out = final[:]
			}
			base := i * 16

			s0 := inHigh[0] + inHigh[7]
			s1 := inHigh[1] + inHigh[6]
			s2 := inHigh[2] + inHigh[5]
			s3 := inHigh[3] + inHigh[4]
			s4 := inHigh[3] - inHigh[4]
			s5 := inHigh[2] - inHigh[5]
			s6 := inHigh[1] - inHigh[6]
			s7 := inHigh[0] - inHigh[7]

			x0 := s0 + s3
			x1 := s1 + s2
			x2 := s1 - s2
			x3 := s0 - s3
			out[base+0] = fdctRoundShift((x0 + x1) * fdctCospi16_64)
			out[base+4] = fdctRoundShift(x3*fdctCospi8_64 + x2*fdctCospi24_64)
			out[base+8] = fdctRoundShift((x0 - x1) * fdctCospi16_64)
			out[base+12] = fdctRoundShift(x3*fdctCospi24_64 - x2*fdctCospi8_64)

			t0 := (s6 - s5) * fdctCospi16_64
			t1 := (s6 + s5) * fdctCospi16_64
			t2 := fdctRoundShift(t0)
			t3 := fdctRoundShift(t1)
			x0 = s4 + t2
			x1 = s4 - t2
			x2 = s7 - t3
			x3 = s7 + t3
			out[base+2] = fdctRoundShift(x0*fdctCospi28_64 + x3*fdctCospi4_64)
			out[base+6] = fdctRoundShift(x2*fdctCospi12_64 - x1*fdctCospi20_64)
			out[base+10] = fdctRoundShift(x1*fdctCospi12_64 + x2*fdctCospi20_64)
			out[base+14] = fdctRoundShift(x3*fdctCospi28_64 - x0*fdctCospi4_64)

			step2[2] = fdctRoundShift((step1[5] - step1[2]) * fdctCospi16_64)
			step2[3] = fdctRoundShift((step1[4] - step1[3]) * fdctCospi16_64)
			step2[4] = fdctRoundShift((step1[4] + step1[3]) * fdctCospi16_64)
			step2[5] = fdctRoundShift((step1[5] + step1[2]) * fdctCospi16_64)

			step3[0] = step1[0] + step2[3]
			step3[1] = step1[1] + step2[2]
			step3[2] = step1[1] - step2[2]
			step3[3] = step1[0] - step2[3]
			step3[4] = step1[7] - step2[4]
			step3[5] = step1[6] - step2[5]
			step3[6] = step1[6] + step2[5]
			step3[7] = step1[7] + step2[4]

			step2[1] = fdctRoundShift(-step3[1]*fdctCospi8_64 + step3[6]*fdctCospi24_64)
			step2[2] = fdctRoundShift(step3[2]*fdctCospi24_64 + step3[5]*fdctCospi8_64)
			step2[5] = fdctRoundShift(step3[2]*fdctCospi8_64 - step3[5]*fdctCospi24_64)
			step2[6] = fdctRoundShift(step3[1]*fdctCospi24_64 + step3[6]*fdctCospi8_64)

			step1[0] = step3[0] + step2[1]
			step1[1] = step3[0] - step2[1]
			step1[2] = step3[3] + step2[2]
			step1[3] = step3[3] - step2[2]
			step1[4] = step3[4] - step2[5]
			step1[5] = step3[4] + step2[5]
			step1[6] = step3[7] - step2[6]
			step1[7] = step3[7] + step2[6]

			out[base+1] = fdctRoundShift(step1[0]*fdctCospi30_64 + step1[7]*fdctCospi2_64)
			out[base+9] = fdctRoundShift(step1[1]*fdctCospi14_64 + step1[6]*fdctCospi18_64)
			out[base+5] = fdctRoundShift(step1[2]*fdctCospi22_64 + step1[5]*fdctCospi10_64)
			out[base+13] = fdctRoundShift(step1[3]*fdctCospi6_64 + step1[4]*fdctCospi26_64)
			out[base+3] = fdctRoundShift(-step1[3]*fdctCospi26_64 + step1[4]*fdctCospi6_64)
			out[base+11] = fdctRoundShift(-step1[2]*fdctCospi10_64 + step1[5]*fdctCospi22_64)
			out[base+7] = fdctRoundShift(-step1[1]*fdctCospi18_64 + step1[6]*fdctCospi14_64)
			out[base+15] = fdctRoundShift(-step1[0]*fdctCospi2_64 + step1[7]*fdctCospi30_64)
		}
	}

	for i := range 256 {
		output[i] = int16(final[i])
	}
}

// ForwardHT4x4Into mirrors libvpx v1.16.0 vp9_fht4x4_c. txType selects the
// DCT/ADST pair for an intra luma transform; DCT_DCT delegates to the regular
// forward DCT path.
func ForwardHT4x4Into(input []int16, stride int, txType common.TxType, output []int16) {
	if txType == common.DctDct {
		ForwardDCT4x4Into(input, stride, output)
		return
	}
	cols, rows, ok := forwardHybridTxKernels(txType, forwardDCT4, forwardADST4)
	if !ok {
		return
	}
	var out [16]int
	var tempIn, tempOut [4]int
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			tempIn[j] = int(input[j*stride+i]) * 16
		}
		if i == 0 && tempIn[0] != 0 {
			tempIn[0]++
		}
		cols(tempIn[:], tempOut[:])
		for j := 0; j < 4; j++ {
			out[j*4+i] = tempOut[j]
		}
	}
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			tempIn[j] = out[j+i*4]
		}
		rows(tempIn[:], tempOut[:])
		for j := 0; j < 4; j++ {
			output[j+i*4] = int16((tempOut[j] + 1) >> 2)
		}
	}
}

// ForwardHT8x8Into mirrors libvpx v1.16.0 vp9_fht8x8_c.
func ForwardHT8x8Into(input []int16, stride int, txType common.TxType, output []int16) {
	if txType == common.DctDct {
		ForwardDCT8x8Into(input, stride, output)
		return
	}
	cols, rows, ok := forwardHybridTxKernels(txType, forwardDCT8, forwardADST8)
	if !ok {
		return
	}
	var out [64]int
	var tempIn, tempOut [8]int
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			tempIn[j] = int(input[j*stride+i]) * 4
		}
		cols(tempIn[:], tempOut[:])
		for j := 0; j < 8; j++ {
			out[j*8+i] = tempOut[j]
		}
	}
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			tempIn[j] = out[j+i*8]
		}
		rows(tempIn[:], tempOut[:])
		for j := 0; j < 8; j++ {
			output[j+i*8] = int16((tempOut[j] + fdctBoolInt(tempOut[j] < 0)) >> 1)
		}
	}
}

// ForwardHT16x16Into mirrors libvpx v1.16.0 vp9_fht16x16_c.
func ForwardHT16x16Into(input []int16, stride int, txType common.TxType, output []int16) {
	if txType == common.DctDct {
		ForwardDCT16x16Into(input, stride, output)
		return
	}
	cols, rows, ok := forwardHybridTxKernels(txType, forwardDCT16, forwardADST16)
	if !ok {
		return
	}
	var out [256]int
	var tempIn, tempOut [16]int
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			tempIn[j] = int(input[j*stride+i]) * 4
		}
		cols(tempIn[:], tempOut[:])
		for j := 0; j < 16; j++ {
			out[j*16+i] = (tempOut[j] + 1 + fdctBoolInt(tempOut[j] < 0)) >> 2
		}
	}
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			tempIn[j] = out[j+i*16]
		}
		rows(tempIn[:], tempOut[:])
		for j := 0; j < 16; j++ {
			output[j+i*16] = int16(tempOut[j])
		}
	}
}

type forwardTx1DKernel func(input, output []int)

func forwardHybridTxKernels(txType common.TxType, dct, adst forwardTx1DKernel) (
	forwardTx1DKernel, forwardTx1DKernel, bool,
) {
	switch txType {
	case common.DctDct:
		return dct, dct, true
	case common.AdstDct:
		return adst, dct, true
	case common.DctAdst:
		return dct, adst, true
	case common.AdstAdst:
		return adst, adst, true
	default:
		return nil, nil, false
	}
}

func forwardDCT4(input, output []int) {
	step0 := input[0] + input[3]
	step1 := input[1] + input[2]
	step2 := input[1] - input[2]
	step3 := input[0] - input[3]

	output[0] = fdctRoundShift((step0 + step1) * fdctCospi16_64)
	output[2] = fdctRoundShift((step0 - step1) * fdctCospi16_64)
	output[1] = fdctRoundShift(step2*fdctCospi24_64 + step3*fdctCospi8_64)
	output[3] = fdctRoundShift(-step2*fdctCospi8_64 + step3*fdctCospi24_64)
}

func forwardDCT8(input, output []int) {
	s0 := input[0] + input[7]
	s1 := input[1] + input[6]
	s2 := input[2] + input[5]
	s3 := input[3] + input[4]
	s4 := input[3] - input[4]
	s5 := input[2] - input[5]
	s6 := input[1] - input[6]
	s7 := input[0] - input[7]

	x0 := s0 + s3
	x1 := s1 + s2
	x2 := s1 - s2
	x3 := s0 - s3
	output[0] = fdctRoundShift((x0 + x1) * fdctCospi16_64)
	output[2] = fdctRoundShift(x2*fdctCospi24_64 + x3*fdctCospi8_64)
	output[4] = fdctRoundShift((x0 - x1) * fdctCospi16_64)
	output[6] = fdctRoundShift(-x2*fdctCospi8_64 + x3*fdctCospi24_64)

	t2 := fdctRoundShift((s6 - s5) * fdctCospi16_64)
	t3 := fdctRoundShift((s6 + s5) * fdctCospi16_64)
	x0 = s4 + t2
	x1 = s4 - t2
	x2 = s7 - t3
	x3 = s7 + t3

	output[1] = fdctRoundShift(x0*fdctCospi28_64 + x3*fdctCospi4_64)
	output[3] = fdctRoundShift(x2*fdctCospi12_64 - x1*fdctCospi20_64)
	output[5] = fdctRoundShift(x1*fdctCospi12_64 + x2*fdctCospi20_64)
	output[7] = fdctRoundShift(x3*fdctCospi28_64 - x0*fdctCospi4_64)
}

func forwardDCT16(in, out []int) {
	var step1, step2, step3, input [8]int
	input[0] = in[0] + in[15]
	input[1] = in[1] + in[14]
	input[2] = in[2] + in[13]
	input[3] = in[3] + in[12]
	input[4] = in[4] + in[11]
	input[5] = in[5] + in[10]
	input[6] = in[6] + in[9]
	input[7] = in[7] + in[8]

	step1[0] = in[7] - in[8]
	step1[1] = in[6] - in[9]
	step1[2] = in[5] - in[10]
	step1[3] = in[4] - in[11]
	step1[4] = in[3] - in[12]
	step1[5] = in[2] - in[13]
	step1[6] = in[1] - in[14]
	step1[7] = in[0] - in[15]

	var dct8Out [8]int
	forwardDCT8(input[:], dct8Out[:])
	out[0] = dct8Out[0]
	out[4] = dct8Out[2]
	out[8] = dct8Out[4]
	out[12] = dct8Out[6]
	out[2] = dct8Out[1]
	out[6] = dct8Out[3]
	out[10] = dct8Out[5]
	out[14] = dct8Out[7]

	step2[2] = fdctRoundShift((step1[5] - step1[2]) * fdctCospi16_64)
	step2[3] = fdctRoundShift((step1[4] - step1[3]) * fdctCospi16_64)
	step2[4] = fdctRoundShift((step1[4] + step1[3]) * fdctCospi16_64)
	step2[5] = fdctRoundShift((step1[5] + step1[2]) * fdctCospi16_64)

	step3[0] = step1[0] + step2[3]
	step3[1] = step1[1] + step2[2]
	step3[2] = step1[1] - step2[2]
	step3[3] = step1[0] - step2[3]
	step3[4] = step1[7] - step2[4]
	step3[5] = step1[6] - step2[5]
	step3[6] = step1[6] + step2[5]
	step3[7] = step1[7] + step2[4]

	step2[1] = fdctRoundShift(-step3[1]*fdctCospi8_64 + step3[6]*fdctCospi24_64)
	step2[2] = fdctRoundShift(step3[2]*fdctCospi24_64 + step3[5]*fdctCospi8_64)
	step2[5] = fdctRoundShift(step3[2]*fdctCospi8_64 - step3[5]*fdctCospi24_64)
	step2[6] = fdctRoundShift(step3[1]*fdctCospi24_64 + step3[6]*fdctCospi8_64)

	step1[0] = step3[0] + step2[1]
	step1[1] = step3[0] - step2[1]
	step1[2] = step3[3] + step2[2]
	step1[3] = step3[3] - step2[2]
	step1[4] = step3[4] - step2[5]
	step1[5] = step3[4] + step2[5]
	step1[6] = step3[7] - step2[6]
	step1[7] = step3[7] + step2[6]

	out[1] = fdctRoundShift(step1[0]*fdctCospi30_64 + step1[7]*fdctCospi2_64)
	out[9] = fdctRoundShift(step1[1]*fdctCospi14_64 + step1[6]*fdctCospi18_64)
	out[5] = fdctRoundShift(step1[2]*fdctCospi22_64 + step1[5]*fdctCospi10_64)
	out[13] = fdctRoundShift(step1[3]*fdctCospi6_64 + step1[4]*fdctCospi26_64)
	out[3] = fdctRoundShift(-step1[3]*fdctCospi26_64 + step1[4]*fdctCospi6_64)
	out[11] = fdctRoundShift(-step1[2]*fdctCospi10_64 + step1[5]*fdctCospi22_64)
	out[7] = fdctRoundShift(-step1[1]*fdctCospi18_64 + step1[6]*fdctCospi14_64)
	out[15] = fdctRoundShift(-step1[0]*fdctCospi2_64 + step1[7]*fdctCospi30_64)
}

func forwardADST4(input, output []int) {
	x0, x1, x2, x3 := input[0], input[1], input[2], input[3]
	if x0|x1|x2|x3 == 0 {
		output[0], output[1], output[2], output[3] = 0, 0, 0, 0
		return
	}
	s0 := fdctSinpi1_9 * x0
	s1 := fdctSinpi4_9 * x0
	s2 := fdctSinpi2_9 * x1
	s3 := fdctSinpi1_9 * x1
	s4 := fdctSinpi3_9 * x2
	s5 := fdctSinpi4_9 * x3
	s6 := fdctSinpi2_9 * x3
	s7 := x0 + x1 - x3

	x0 = s0 + s2 + s5
	x1 = fdctSinpi3_9 * s7
	x2 = s1 - s3 + s6
	x3 = s4

	s0 = x0 + x3
	s1 = x1
	s2 = x2 - x3
	s3 = x2 - x0 + x3

	output[0] = fdctRoundShift(s0)
	output[1] = fdctRoundShift(s1)
	output[2] = fdctRoundShift(s2)
	output[3] = fdctRoundShift(s3)
}

func forwardADST8(input, output []int) {
	x0 := input[7]
	x1 := input[0]
	x2 := input[5]
	x3 := input[2]
	x4 := input[3]
	x5 := input[4]
	x6 := input[1]
	x7 := input[6]

	s0 := fdctCospi2_64*x0 + fdctCospi30_64*x1
	s1 := fdctCospi30_64*x0 - fdctCospi2_64*x1
	s2 := fdctCospi10_64*x2 + fdctCospi22_64*x3
	s3 := fdctCospi22_64*x2 - fdctCospi10_64*x3
	s4 := fdctCospi18_64*x4 + fdctCospi14_64*x5
	s5 := fdctCospi14_64*x4 - fdctCospi18_64*x5
	s6 := fdctCospi26_64*x6 + fdctCospi6_64*x7
	s7 := fdctCospi6_64*x6 - fdctCospi26_64*x7

	x0 = fdctRoundShift(s0 + s4)
	x1 = fdctRoundShift(s1 + s5)
	x2 = fdctRoundShift(s2 + s6)
	x3 = fdctRoundShift(s3 + s7)
	x4 = fdctRoundShift(s0 - s4)
	x5 = fdctRoundShift(s1 - s5)
	x6 = fdctRoundShift(s2 - s6)
	x7 = fdctRoundShift(s3 - s7)

	s0 = x0
	s1 = x1
	s2 = x2
	s3 = x3
	s4 = fdctCospi8_64*x4 + fdctCospi24_64*x5
	s5 = fdctCospi24_64*x4 - fdctCospi8_64*x5
	s6 = -fdctCospi24_64*x6 + fdctCospi8_64*x7
	s7 = fdctCospi8_64*x6 + fdctCospi24_64*x7

	x0 = s0 + s2
	x1 = s1 + s3
	x2 = s0 - s2
	x3 = s1 - s3
	x4 = fdctRoundShift(s4 + s6)
	x5 = fdctRoundShift(s5 + s7)
	x6 = fdctRoundShift(s4 - s6)
	x7 = fdctRoundShift(s5 - s7)

	s2 = fdctCospi16_64 * (x2 + x3)
	s3 = fdctCospi16_64 * (x2 - x3)
	s6 = fdctCospi16_64 * (x6 + x7)
	s7 = fdctCospi16_64 * (x6 - x7)

	x2 = fdctRoundShift(s2)
	x3 = fdctRoundShift(s3)
	x6 = fdctRoundShift(s6)
	x7 = fdctRoundShift(s7)

	output[0] = x0
	output[1] = -x4
	output[2] = x6
	output[3] = -x2
	output[4] = x3
	output[5] = -x7
	output[6] = x5
	output[7] = -x1
}

func forwardADST16(input, output []int) {
	x0 := input[15]
	x1 := input[0]
	x2 := input[13]
	x3 := input[2]
	x4 := input[11]
	x5 := input[4]
	x6 := input[9]
	x7 := input[6]
	x8 := input[7]
	x9 := input[8]
	x10 := input[5]
	x11 := input[10]
	x12 := input[3]
	x13 := input[12]
	x14 := input[1]
	x15 := input[14]

	s0 := x0*fdctCospi1_64 + x1*fdctCospi31_64
	s1 := x0*fdctCospi31_64 - x1*fdctCospi1_64
	s2 := x2*fdctCospi5_64 + x3*fdctCospi27_64
	s3 := x2*fdctCospi27_64 - x3*fdctCospi5_64
	s4 := x4*fdctCospi9_64 + x5*fdctCospi23_64
	s5 := x4*fdctCospi23_64 - x5*fdctCospi9_64
	s6 := x6*fdctCospi13_64 + x7*fdctCospi19_64
	s7 := x6*fdctCospi19_64 - x7*fdctCospi13_64
	s8 := x8*fdctCospi17_64 + x9*fdctCospi15_64
	s9 := x8*fdctCospi15_64 - x9*fdctCospi17_64
	s10 := x10*fdctCospi21_64 + x11*fdctCospi11_64
	s11 := x10*fdctCospi11_64 - x11*fdctCospi21_64
	s12 := x12*fdctCospi25_64 + x13*fdctCospi7_64
	s13 := x12*fdctCospi7_64 - x13*fdctCospi25_64
	s14 := x14*fdctCospi29_64 + x15*fdctCospi3_64
	s15 := x14*fdctCospi3_64 - x15*fdctCospi29_64

	x0 = fdctRoundShift(s0 + s8)
	x1 = fdctRoundShift(s1 + s9)
	x2 = fdctRoundShift(s2 + s10)
	x3 = fdctRoundShift(s3 + s11)
	x4 = fdctRoundShift(s4 + s12)
	x5 = fdctRoundShift(s5 + s13)
	x6 = fdctRoundShift(s6 + s14)
	x7 = fdctRoundShift(s7 + s15)
	x8 = fdctRoundShift(s0 - s8)
	x9 = fdctRoundShift(s1 - s9)
	x10 = fdctRoundShift(s2 - s10)
	x11 = fdctRoundShift(s3 - s11)
	x12 = fdctRoundShift(s4 - s12)
	x13 = fdctRoundShift(s5 - s13)
	x14 = fdctRoundShift(s6 - s14)
	x15 = fdctRoundShift(s7 - s15)

	s0 = x0
	s1 = x1
	s2 = x2
	s3 = x3
	s4 = x4
	s5 = x5
	s6 = x6
	s7 = x7
	s8 = x8*fdctCospi4_64 + x9*fdctCospi28_64
	s9 = x8*fdctCospi28_64 - x9*fdctCospi4_64
	s10 = x10*fdctCospi20_64 + x11*fdctCospi12_64
	s11 = x10*fdctCospi12_64 - x11*fdctCospi20_64
	s12 = -x12*fdctCospi28_64 + x13*fdctCospi4_64
	s13 = x12*fdctCospi4_64 + x13*fdctCospi28_64
	s14 = -x14*fdctCospi12_64 + x15*fdctCospi20_64
	s15 = x14*fdctCospi20_64 + x15*fdctCospi12_64

	x0 = s0 + s4
	x1 = s1 + s5
	x2 = s2 + s6
	x3 = s3 + s7
	x4 = s0 - s4
	x5 = s1 - s5
	x6 = s2 - s6
	x7 = s3 - s7
	x8 = fdctRoundShift(s8 + s12)
	x9 = fdctRoundShift(s9 + s13)
	x10 = fdctRoundShift(s10 + s14)
	x11 = fdctRoundShift(s11 + s15)
	x12 = fdctRoundShift(s8 - s12)
	x13 = fdctRoundShift(s9 - s13)
	x14 = fdctRoundShift(s10 - s14)
	x15 = fdctRoundShift(s11 - s15)

	s0 = x0
	s1 = x1
	s2 = x2
	s3 = x3
	s4 = x4*fdctCospi8_64 + x5*fdctCospi24_64
	s5 = x4*fdctCospi24_64 - x5*fdctCospi8_64
	s6 = -x6*fdctCospi24_64 + x7*fdctCospi8_64
	s7 = x6*fdctCospi8_64 + x7*fdctCospi24_64
	s8 = x8
	s9 = x9
	s10 = x10
	s11 = x11
	s12 = x12*fdctCospi8_64 + x13*fdctCospi24_64
	s13 = x12*fdctCospi24_64 - x13*fdctCospi8_64
	s14 = -x14*fdctCospi24_64 + x15*fdctCospi8_64
	s15 = x14*fdctCospi8_64 + x15*fdctCospi24_64

	x0 = s0 + s2
	x1 = s1 + s3
	x2 = s0 - s2
	x3 = s1 - s3
	x4 = fdctRoundShift(s4 + s6)
	x5 = fdctRoundShift(s5 + s7)
	x6 = fdctRoundShift(s4 - s6)
	x7 = fdctRoundShift(s5 - s7)
	x8 = s8 + s10
	x9 = s9 + s11
	x10 = s8 - s10
	x11 = s9 - s11
	x12 = fdctRoundShift(s12 + s14)
	x13 = fdctRoundShift(s13 + s15)
	x14 = fdctRoundShift(s12 - s14)
	x15 = fdctRoundShift(s13 - s15)

	s2 = -fdctCospi16_64 * (x2 + x3)
	s3 = fdctCospi16_64 * (x2 - x3)
	s6 = fdctCospi16_64 * (x6 + x7)
	s7 = fdctCospi16_64 * (-x6 + x7)
	s10 = fdctCospi16_64 * (x10 + x11)
	s11 = fdctCospi16_64 * (-x10 + x11)
	s14 = -fdctCospi16_64 * (x14 + x15)
	s15 = fdctCospi16_64 * (x14 - x15)

	x2 = fdctRoundShift(s2)
	x3 = fdctRoundShift(s3)
	x6 = fdctRoundShift(s6)
	x7 = fdctRoundShift(s7)
	x10 = fdctRoundShift(s10)
	x11 = fdctRoundShift(s11)
	x14 = fdctRoundShift(s14)
	x15 = fdctRoundShift(s15)

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

// ForwardDCT32x32 mirrors libvpx v1.16.0 vpx_fdct32x32_c. Input is a
// 32x32 residual block with caller-provided stride; output is raster-order
// transform coefficients.
func ForwardDCT32x32(input []int16, stride int, output *[1024]int16) {
	ForwardDCT32x32Into(input, stride, output[:])
}

// ForwardDCT32x32Into is the slice-backed form of ForwardDCT32x32. output
// must hold at least 1024 coefficients.
func ForwardDCT32x32Into(input []int16, stride int, output []int16) {
	var intermediate [1024]int
	var tempIn, tempOut [32]int

	for i := 0; i < 32; i++ {
		for j := 0; j < 32; j++ {
			tempIn[j] = int(input[j*stride+i]) * 4
		}
		forwardDCT32(tempIn[:], tempOut[:], false)
		for j := 0; j < 32; j++ {
			intermediate[j*32+i] = (tempOut[j] + 1 + fdctBoolInt(tempOut[j] > 0)) >> 2
		}
	}

	for i := 0; i < 32; i++ {
		for j := 0; j < 32; j++ {
			tempIn[j] = intermediate[j+i*32]
		}
		forwardDCT32(tempIn[:], tempOut[:], false)
		for j := 0; j < 32; j++ {
			output[j+i*32] = int16((tempOut[j] + 1 + fdctBoolInt(tempOut[j] < 0)) >> 2)
		}
	}
}

func forwardDCT32(input []int, output []int, round bool) {
	var step [32]int

	// Stage 1
	step[0] = input[0] + input[31]
	step[1] = input[1] + input[30]
	step[2] = input[2] + input[29]
	step[3] = input[3] + input[28]
	step[4] = input[4] + input[27]
	step[5] = input[5] + input[26]
	step[6] = input[6] + input[25]
	step[7] = input[7] + input[24]
	step[8] = input[8] + input[23]
	step[9] = input[9] + input[22]
	step[10] = input[10] + input[21]
	step[11] = input[11] + input[20]
	step[12] = input[12] + input[19]
	step[13] = input[13] + input[18]
	step[14] = input[14] + input[17]
	step[15] = input[15] + input[16]
	step[16] = -input[16] + input[15]
	step[17] = -input[17] + input[14]
	step[18] = -input[18] + input[13]
	step[19] = -input[19] + input[12]
	step[20] = -input[20] + input[11]
	step[21] = -input[21] + input[10]
	step[22] = -input[22] + input[9]
	step[23] = -input[23] + input[8]
	step[24] = -input[24] + input[7]
	step[25] = -input[25] + input[6]
	step[26] = -input[26] + input[5]
	step[27] = -input[27] + input[4]
	step[28] = -input[28] + input[3]
	step[29] = -input[29] + input[2]
	step[30] = -input[30] + input[1]
	step[31] = -input[31] + input[0]

	// Stage 2
	output[0] = step[0] + step[15]
	output[1] = step[1] + step[14]
	output[2] = step[2] + step[13]
	output[3] = step[3] + step[12]
	output[4] = step[4] + step[11]
	output[5] = step[5] + step[10]
	output[6] = step[6] + step[9]
	output[7] = step[7] + step[8]
	output[8] = -step[8] + step[7]
	output[9] = -step[9] + step[6]
	output[10] = -step[10] + step[5]
	output[11] = -step[11] + step[4]
	output[12] = -step[12] + step[3]
	output[13] = -step[13] + step[2]
	output[14] = -step[14] + step[1]
	output[15] = -step[15] + step[0]
	output[16] = step[16]
	output[17] = step[17]
	output[18] = step[18]
	output[19] = step[19]
	output[20] = fdctRoundShift((-step[20] + step[27]) * fdctCospi16_64)
	output[21] = fdctRoundShift((-step[21] + step[26]) * fdctCospi16_64)
	output[22] = fdctRoundShift((-step[22] + step[25]) * fdctCospi16_64)
	output[23] = fdctRoundShift((-step[23] + step[24]) * fdctCospi16_64)
	output[24] = fdctRoundShift((step[24] + step[23]) * fdctCospi16_64)
	output[25] = fdctRoundShift((step[25] + step[22]) * fdctCospi16_64)
	output[26] = fdctRoundShift((step[26] + step[21]) * fdctCospi16_64)
	output[27] = fdctRoundShift((step[27] + step[20]) * fdctCospi16_64)
	output[28] = step[28]
	output[29] = step[29]
	output[30] = step[30]
	output[31] = step[31]

	if round {
		for i := 0; i < 32; i++ {
			output[i] = fdctHalfRoundShift(output[i])
		}
	}

	// Stage 3
	step[0] = output[0] + output[7]
	step[1] = output[1] + output[6]
	step[2] = output[2] + output[5]
	step[3] = output[3] + output[4]
	step[4] = -output[4] + output[3]
	step[5] = -output[5] + output[2]
	step[6] = -output[6] + output[1]
	step[7] = -output[7] + output[0]
	step[8] = output[8]
	step[9] = output[9]
	step[10] = fdctRoundShift((-output[10] + output[13]) * fdctCospi16_64)
	step[11] = fdctRoundShift((-output[11] + output[12]) * fdctCospi16_64)
	step[12] = fdctRoundShift((output[12] + output[11]) * fdctCospi16_64)
	step[13] = fdctRoundShift((output[13] + output[10]) * fdctCospi16_64)
	step[14] = output[14]
	step[15] = output[15]
	step[16] = output[16] + output[23]
	step[17] = output[17] + output[22]
	step[18] = output[18] + output[21]
	step[19] = output[19] + output[20]
	step[20] = -output[20] + output[19]
	step[21] = -output[21] + output[18]
	step[22] = -output[22] + output[17]
	step[23] = -output[23] + output[16]
	step[24] = -output[24] + output[31]
	step[25] = -output[25] + output[30]
	step[26] = -output[26] + output[29]
	step[27] = -output[27] + output[28]
	step[28] = output[28] + output[27]
	step[29] = output[29] + output[26]
	step[30] = output[30] + output[25]
	step[31] = output[31] + output[24]

	// Stage 4
	output[0] = step[0] + step[3]
	output[1] = step[1] + step[2]
	output[2] = -step[2] + step[1]
	output[3] = -step[3] + step[0]
	output[4] = step[4]
	output[5] = fdctRoundShift((-step[5] + step[6]) * fdctCospi16_64)
	output[6] = fdctRoundShift((step[6] + step[5]) * fdctCospi16_64)
	output[7] = step[7]
	output[8] = step[8] + step[11]
	output[9] = step[9] + step[10]
	output[10] = -step[10] + step[9]
	output[11] = -step[11] + step[8]
	output[12] = -step[12] + step[15]
	output[13] = -step[13] + step[14]
	output[14] = step[14] + step[13]
	output[15] = step[15] + step[12]
	output[16] = step[16]
	output[17] = step[17]
	output[18] = fdctRoundShift(step[18]*-fdctCospi8_64 + step[29]*fdctCospi24_64)
	output[19] = fdctRoundShift(step[19]*-fdctCospi8_64 + step[28]*fdctCospi24_64)
	output[20] = fdctRoundShift(step[20]*-fdctCospi24_64 + step[27]*-fdctCospi8_64)
	output[21] = fdctRoundShift(step[21]*-fdctCospi24_64 + step[26]*-fdctCospi8_64)
	output[22] = step[22]
	output[23] = step[23]
	output[24] = step[24]
	output[25] = step[25]
	output[26] = fdctRoundShift(step[26]*fdctCospi24_64 + step[21]*-fdctCospi8_64)
	output[27] = fdctRoundShift(step[27]*fdctCospi24_64 + step[20]*-fdctCospi8_64)
	output[28] = fdctRoundShift(step[28]*fdctCospi8_64 + step[19]*fdctCospi24_64)
	output[29] = fdctRoundShift(step[29]*fdctCospi8_64 + step[18]*fdctCospi24_64)
	output[30] = step[30]
	output[31] = step[31]

	// Stage 5
	step[0] = fdctRoundShift((output[0] + output[1]) * fdctCospi16_64)
	step[1] = fdctRoundShift((-output[1] + output[0]) * fdctCospi16_64)
	step[2] = fdctRoundShift(output[2]*fdctCospi24_64 + output[3]*fdctCospi8_64)
	step[3] = fdctRoundShift(output[3]*fdctCospi24_64 - output[2]*fdctCospi8_64)
	step[4] = output[4] + output[5]
	step[5] = -output[5] + output[4]
	step[6] = -output[6] + output[7]
	step[7] = output[7] + output[6]
	step[8] = output[8]
	step[9] = fdctRoundShift(output[9]*-fdctCospi8_64 + output[14]*fdctCospi24_64)
	step[10] = fdctRoundShift(output[10]*-fdctCospi24_64 + output[13]*-fdctCospi8_64)
	step[11] = output[11]
	step[12] = output[12]
	step[13] = fdctRoundShift(output[13]*fdctCospi24_64 + output[10]*-fdctCospi8_64)
	step[14] = fdctRoundShift(output[14]*fdctCospi8_64 + output[9]*fdctCospi24_64)
	step[15] = output[15]
	step[16] = output[16] + output[19]
	step[17] = output[17] + output[18]
	step[18] = -output[18] + output[17]
	step[19] = -output[19] + output[16]
	step[20] = -output[20] + output[23]
	step[21] = -output[21] + output[22]
	step[22] = output[22] + output[21]
	step[23] = output[23] + output[20]
	step[24] = output[24] + output[27]
	step[25] = output[25] + output[26]
	step[26] = -output[26] + output[25]
	step[27] = -output[27] + output[24]
	step[28] = -output[28] + output[31]
	step[29] = -output[29] + output[30]
	step[30] = output[30] + output[29]
	step[31] = output[31] + output[28]

	// Stage 6
	output[0] = step[0]
	output[1] = step[1]
	output[2] = step[2]
	output[3] = step[3]
	output[4] = fdctRoundShift(step[4]*fdctCospi28_64 + step[7]*fdctCospi4_64)
	output[5] = fdctRoundShift(step[5]*fdctCospi12_64 + step[6]*fdctCospi20_64)
	output[6] = fdctRoundShift(step[6]*fdctCospi12_64 + step[5]*-fdctCospi20_64)
	output[7] = fdctRoundShift(step[7]*fdctCospi28_64 + step[4]*-fdctCospi4_64)
	output[8] = step[8] + step[9]
	output[9] = -step[9] + step[8]
	output[10] = -step[10] + step[11]
	output[11] = step[11] + step[10]
	output[12] = step[12] + step[13]
	output[13] = -step[13] + step[12]
	output[14] = -step[14] + step[15]
	output[15] = step[15] + step[14]
	output[16] = step[16]
	output[17] = fdctRoundShift(step[17]*-fdctCospi4_64 + step[30]*fdctCospi28_64)
	output[18] = fdctRoundShift(step[18]*-fdctCospi28_64 + step[29]*-fdctCospi4_64)
	output[19] = step[19]
	output[20] = step[20]
	output[21] = fdctRoundShift(step[21]*-fdctCospi20_64 + step[26]*fdctCospi12_64)
	output[22] = fdctRoundShift(step[22]*-fdctCospi12_64 + step[25]*-fdctCospi20_64)
	output[23] = step[23]
	output[24] = step[24]
	output[25] = fdctRoundShift(step[25]*fdctCospi12_64 + step[22]*-fdctCospi20_64)
	output[26] = fdctRoundShift(step[26]*fdctCospi20_64 + step[21]*fdctCospi12_64)
	output[27] = step[27]
	output[28] = step[28]
	output[29] = fdctRoundShift(step[29]*fdctCospi28_64 + step[18]*-fdctCospi4_64)
	output[30] = fdctRoundShift(step[30]*fdctCospi4_64 + step[17]*fdctCospi28_64)
	output[31] = step[31]

	// Stage 7
	step[0] = output[0]
	step[1] = output[1]
	step[2] = output[2]
	step[3] = output[3]
	step[4] = output[4]
	step[5] = output[5]
	step[6] = output[6]
	step[7] = output[7]
	step[8] = fdctRoundShift(output[8]*fdctCospi30_64 + output[15]*fdctCospi2_64)
	step[9] = fdctRoundShift(output[9]*fdctCospi14_64 + output[14]*fdctCospi18_64)
	step[10] = fdctRoundShift(output[10]*fdctCospi22_64 + output[13]*fdctCospi10_64)
	step[11] = fdctRoundShift(output[11]*fdctCospi6_64 + output[12]*fdctCospi26_64)
	step[12] = fdctRoundShift(output[12]*fdctCospi6_64 + output[11]*-fdctCospi26_64)
	step[13] = fdctRoundShift(output[13]*fdctCospi22_64 + output[10]*-fdctCospi10_64)
	step[14] = fdctRoundShift(output[14]*fdctCospi14_64 + output[9]*-fdctCospi18_64)
	step[15] = fdctRoundShift(output[15]*fdctCospi30_64 + output[8]*-fdctCospi2_64)
	step[16] = output[16] + output[17]
	step[17] = -output[17] + output[16]
	step[18] = -output[18] + output[19]
	step[19] = output[19] + output[18]
	step[20] = output[20] + output[21]
	step[21] = -output[21] + output[20]
	step[22] = -output[22] + output[23]
	step[23] = output[23] + output[22]
	step[24] = output[24] + output[25]
	step[25] = -output[25] + output[24]
	step[26] = -output[26] + output[27]
	step[27] = output[27] + output[26]
	step[28] = output[28] + output[29]
	step[29] = -output[29] + output[28]
	step[30] = -output[30] + output[31]
	step[31] = output[31] + output[30]

	output[0] = step[0]
	output[16] = step[1]
	output[8] = step[2]
	output[24] = step[3]
	output[4] = step[4]
	output[20] = step[5]
	output[12] = step[6]
	output[28] = step[7]
	output[2] = step[8]
	output[18] = step[9]
	output[10] = step[10]
	output[26] = step[11]
	output[6] = step[12]
	output[22] = step[13]
	output[14] = step[14]
	output[30] = step[15]
	output[1] = fdctRoundShift(step[16]*fdctCospi31_64 + step[31]*fdctCospi1_64)
	output[17] = fdctRoundShift(step[17]*fdctCospi15_64 + step[30]*fdctCospi17_64)
	output[9] = fdctRoundShift(step[18]*fdctCospi23_64 + step[29]*fdctCospi9_64)
	output[25] = fdctRoundShift(step[19]*fdctCospi7_64 + step[28]*fdctCospi25_64)
	output[5] = fdctRoundShift(step[20]*fdctCospi27_64 + step[27]*fdctCospi5_64)
	output[21] = fdctRoundShift(step[21]*fdctCospi11_64 + step[26]*fdctCospi21_64)
	output[13] = fdctRoundShift(step[22]*fdctCospi19_64 + step[25]*fdctCospi13_64)
	output[29] = fdctRoundShift(step[23]*fdctCospi3_64 + step[24]*fdctCospi29_64)
	output[3] = fdctRoundShift(step[24]*fdctCospi3_64 + step[23]*-fdctCospi29_64)
	output[19] = fdctRoundShift(step[25]*fdctCospi19_64 + step[22]*-fdctCospi13_64)
	output[11] = fdctRoundShift(step[26]*fdctCospi11_64 + step[21]*-fdctCospi21_64)
	output[27] = fdctRoundShift(step[27]*fdctCospi27_64 + step[20]*-fdctCospi5_64)
	output[7] = fdctRoundShift(step[28]*fdctCospi7_64 + step[19]*-fdctCospi25_64)
	output[23] = fdctRoundShift(step[29]*fdctCospi23_64 + step[18]*-fdctCospi9_64)
	output[15] = fdctRoundShift(step[30]*fdctCospi15_64 + step[17]*-fdctCospi17_64)
	output[31] = fdctRoundShift(step[31]*fdctCospi31_64 + step[16]*-fdctCospi1_64)
}

func fdctRoundShift(input int) int {
	return (input + fdctDctConstRounding) >> fdctDctConstBits
}

func fdctRoundShift2(input int) int {
	return (input + 1) >> 2
}

func fdctHalfRoundShift(input int) int {
	return (input + 1 + fdctBoolInt(input < 0)) >> 2
}

func fdctBoolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// QuantizeFP mirrors libvpx's vp9_quantize_fp_c for non-32x32 transforms.
// dqcoeff receives dequantized coefficients in raster order, which is the
// representation consumed by WriteCoefBlock. The return value is the scan-order
// EOB position.
func QuantizeFP(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	quant := [2]int{(1 << 16) / int(dequant[0]), (1 << 16) / int(dequant[1])}
	round := [2]int{(48 * int(dequant[0])) >> 7, (42 * int(dequant[1])) >> 7}
	eob := -1
	n := min(len(coeff), min(len(scan), len(dqcoeff)))
	for i := range n {
		rc := int(scan[i])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		tmp := clampInt16(absCoeff + round[slot])
		tmp = (tmp * quant[slot]) >> 16
		q := tmp
		if c < 0 {
			q = -q
		}
		dqcoeff[rc] = int16(q * int(dequant[slot]))
		if tmp != 0 {
			eob = i
		}
	}
	return eob + 1
}

// QuantizeFP4x4 mirrors libvpx's vp9_quantize_fp_c for a 4x4 transform.
func QuantizeFP4x4(coeff *[16]int16, dequant [2]int16, scan []int16, dqcoeff *[16]int16) int {
	return QuantizeFP(coeff[:], dequant, scan, dqcoeff[:])
}

// QuantizeFP32x32 mirrors libvpx's vp9_quantize_fp_32x32_c.
func QuantizeFP32x32(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	quant := [2]int{(1 << 16) / int(dequant[0]), (1 << 16) / int(dequant[1])}
	round := [2]int{
		(((48 * int(dequant[0])) >> 7) + 1) >> 1,
		(((42 * int(dequant[1])) >> 7) + 1) >> 1,
	}
	eob := -1
	n := min(len(coeff), min(len(scan), len(dqcoeff)))
	for i := range n {
		rc := int(scan[i])
		slot := 0
		if rc != 0 {
			slot = 1
		}
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}

		tmp := 0
		if absCoeff >= int(dequant[slot])>>2 {
			tmp = clampInt16(absCoeff + round[slot])
			tmp = (tmp * quant[slot]) >> 15
			q := tmp
			if c < 0 {
				q = -q
			}
			dqcoeff[rc] = int16(q * int(dequant[slot]) / 2)
		} else {
			dqcoeff[rc] = 0
		}
		if tmp != 0 {
			eob = i
		}
	}
	return eob + 1
}

func clampInt16(v int) int {
	if v > 32767 {
		return 32767
	}
	return v
}

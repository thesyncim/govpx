package encoder

import (
	"math/bits"

	"github.com/thesyncim/govpx/internal/vp9/common"
)

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
// hold at least 16 coefficients. Dispatches to a SIMD kernel when one is
// available; otherwise drops to the canonical scalar reference.
func ForwardDCT4x4Into(input []int16, stride int, output []int16) {
	forwardDCT4x4Dispatch(input, stride, output)
}

// forwardDCT4x4Scalar is the canonical scalar port of libvpx
// v1.16.0 vpx_fdct4x4_c. SIMD implementations must produce byte-identical
// output for the encoder's residual range.
func forwardDCT4x4Scalar(input []int16, stride int, output []int16) {
	var intermediate [16]int
	var final [16]int

	for pass := range 2 {
		out := intermediate[:]
		if pass == 1 {
			out = final[:]
		}
		for i := range 4 {
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
// normal DCT path. Dispatches to a SIMD kernel when available.
func ForwardWHT4x4Into(input []int16, stride int, output []int16) {
	forwardWHT4x4Dispatch(input, stride, output)
}

// forwardWHT4x4Scalar is the canonical scalar port of libvpx
// v1.16.0 vp9_fwht4x4_c. SIMD implementations must produce byte-identical
// output for the encoder's residual range.
func forwardWHT4x4Scalar(input []int16, stride int, output []int16) {
	var tmp [16]int

	for i := range 4 {
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

	n := min(len(output), 16)
	for i := range n {
		output[i] = 0
	}
	if len(output) < 16 {
		return
	}

	for i := range 4 {
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
// hold at least 64 coefficients. Dispatches to a SIMD kernel when available.
func ForwardDCT8x8Into(input []int16, stride int, output []int16) {
	forwardDCT8x8Dispatch(input, stride, output)
}

// forwardDCT8x8Scalar is the canonical scalar port of libvpx
// v1.16.0 vpx_fdct8x8_c. SIMD implementations must produce byte-identical
// output for the encoder's residual range.
func forwardDCT8x8Scalar(input []int16, stride int, output []int16) {
	var intermediate [64]int
	var final [64]int

	for pass := range 2 {
		for i := range 8 {
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
// must hold at least 256 coefficients. Dispatches to a SIMD kernel when
// available.
func ForwardDCT16x16Into(input []int16, stride int, output []int16) {
	forwardDCT16x16Dispatch(input, stride, output)
}

// forwardDCT16x16Scalar is the canonical scalar port of libvpx
// v1.16.0 vpx_fdct16x16_c. SIMD implementations must produce byte-identical
// output for the encoder's residual range.
func forwardDCT16x16Scalar(input []int16, stride int, output []int16) {
	var intermediate [256]int
	var final [256]int

	for pass := range 2 {
		for i := range 16 {
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
	if txType != common.AdstDct && txType != common.DctAdst && txType != common.AdstAdst {
		return
	}
	var out [16]int
	var tempIn, tempOut [4]int
	for i := range 4 {
		for j := range 4 {
			tempIn[j] = int(input[j*stride+i]) * 16
		}
		if i == 0 && tempIn[0] != 0 {
			tempIn[0]++
		}
		if txType == common.AdstDct || txType == common.AdstAdst {
			forwardADST4(tempIn[:], tempOut[:])
		} else {
			forwardDCT4(tempIn[:], tempOut[:])
		}
		for j := range 4 {
			out[j*4+i] = tempOut[j]
		}
	}
	for i := range 4 {
		for j := range 4 {
			tempIn[j] = out[j+i*4]
		}
		if txType == common.DctAdst || txType == common.AdstAdst {
			forwardADST4(tempIn[:], tempOut[:])
		} else {
			forwardDCT4(tempIn[:], tempOut[:])
		}
		for j := range 4 {
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
	if txType != common.AdstDct && txType != common.DctAdst && txType != common.AdstAdst {
		return
	}
	var out [64]int
	var tempIn, tempOut [8]int
	for i := range 8 {
		for j := range 8 {
			tempIn[j] = int(input[j*stride+i]) * 4
		}
		if txType == common.AdstDct || txType == common.AdstAdst {
			forwardADST8(tempIn[:], tempOut[:])
		} else {
			forwardDCT8(tempIn[:], tempOut[:])
		}
		for j := range 8 {
			out[j*8+i] = tempOut[j]
		}
	}
	for i := range 8 {
		for j := range 8 {
			tempIn[j] = out[j+i*8]
		}
		if txType == common.DctAdst || txType == common.AdstAdst {
			forwardADST8(tempIn[:], tempOut[:])
		} else {
			forwardDCT8(tempIn[:], tempOut[:])
		}
		for j := range 8 {
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
	if txType != common.AdstDct && txType != common.DctAdst && txType != common.AdstAdst {
		return
	}
	var out [256]int
	var tempIn, tempOut [16]int
	for i := range 16 {
		for j := range 16 {
			tempIn[j] = int(input[j*stride+i]) * 4
		}
		if txType == common.AdstDct || txType == common.AdstAdst {
			forwardADST16(tempIn[:], tempOut[:])
		} else {
			forwardDCT16(tempIn[:], tempOut[:])
		}
		for j := range 16 {
			out[j*16+i] = (tempOut[j] + 1 + fdctBoolInt(tempOut[j] < 0)) >> 2
		}
	}
	for i := range 16 {
		for j := range 16 {
			tempIn[j] = out[j+i*16]
		}
		if txType == common.DctAdst || txType == common.AdstAdst {
			forwardADST16(tempIn[:], tempOut[:])
		} else {
			forwardDCT16(tempIn[:], tempOut[:])
		}
		for j := range 16 {
			output[j+i*16] = int16(tempOut[j])
		}
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
// must hold at least 1024 coefficients. Dispatches to a SIMD kernel when
// available.
func ForwardDCT32x32Into(input []int16, stride int, output []int16) {
	forwardDCT32x32Dispatch(input, stride, output)
}

// forwardDCT32x32Scalar is the canonical scalar port of libvpx
// v1.16.0 vpx_fdct32x32_c. SIMD implementations must produce byte-identical
// output for the encoder's residual range.
func forwardDCT32x32Scalar(input []int16, stride int, output []int16) {
	var intermediate [1024]int
	var tempIn, tempOut [32]int

	for i := range 32 {
		for j := range 32 {
			tempIn[j] = int(input[j*stride+i]) * 4
		}
		forwardDCT32(tempIn[:], tempOut[:], false)
		for j := range 32 {
			intermediate[j*32+i] = (tempOut[j] + 1 + fdctBoolInt(tempOut[j] > 0)) >> 2
		}
	}

	for i := range 32 {
		for j := range 32 {
			tempIn[j] = intermediate[j+i*32]
		}
		forwardDCT32(tempIn[:], tempOut[:], false)
		for j := range 32 {
			output[j+i*32] = int16((tempOut[j] + 1 + fdctBoolInt(tempOut[j] < 0)) >> 2)
		}
	}
}

// ForwardDCT32x32RD mirrors libvpx v1.16.0 vpx_fdct32x32_rd_c
// (vpx_dsp/fwd_txfm.c:735). The rate-distortion-loop variant of the 32x32
// forward DCT trades a small amount of precision for speed by issuing
// `dct_32_round` after stage 2 on the row pass (round=1 in forwardDCT32)
// and emitting the row coefficients without a final rounding step. libvpx
// dispatches this variant when MACROBLOCK::use_lp32x32fdct is non-zero
// (see vp9/encoder/vp9_encodemb.c:331-337 and vp9_xform_quant_fp at line
// 396). Input is a 32x32 residual block with caller-provided stride;
// output is raster-order transform coefficients.
func ForwardDCT32x32RD(input []int16, stride int, output *[1024]int16) {
	ForwardDCT32x32RDInto(input, stride, output[:])
}

// ForwardDCT32x32RDInto is the slice-backed form of ForwardDCT32x32RD.
// output must hold at least 1024 coefficients. Dispatches to a SIMD
// kernel when available.
func ForwardDCT32x32RDInto(input []int16, stride int, output []int16) {
	forwardDCT32x32RDDispatch(input, stride, output)
}

// forwardDCT32x32RDScalar is the canonical scalar port of libvpx
// v1.16.0 vpx_fdct32x32_rd_c (vpx_dsp/fwd_txfm.c:735-758). SIMD
// implementations must produce byte-identical output for the encoder's
// residual range.
func forwardDCT32x32RDScalar(input []int16, stride int, output []int16) {
	var intermediate [1024]int
	var tempIn, tempOut [32]int

	// Columns
	for i := range 32 {
		for j := range 32 {
			tempIn[j] = int(input[j*stride+i]) * 4
		}
		forwardDCT32(tempIn[:], tempOut[:], false)
		for j := range 32 {
			// TODO(cd): see quality impact of only doing
			//           intermediate[j * 32 + i] = (tempOut[j] + 1) >> 2;
			//           PS: also change code in vpx_dsp/x86/vpx_dct_sse2.c
			intermediate[j*32+i] = (tempOut[j] + 1 + fdctBoolInt(tempOut[j] > 0)) >> 2
		}
	}

	// Rows
	for i := range 32 {
		for j := range 32 {
			tempIn[j] = intermediate[j+i*32]
		}
		forwardDCT32(tempIn[:], tempOut[:], true)
		for j := range 32 {
			output[j+i*32] = int16(tempOut[j])
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
		for i := range 32 {
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

// QuantizeB mirrors libvpx v1.16.0 vpx_quantize_b_c for 4x4/8x8/16x16
// transforms. qindex is the segment-adjusted quantizer index; dequant is the
// per-plane [DC, AC] dequant pair.
func QuantizeB(coeff []int16, qindex int, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	return QuantizeBWithQ(coeff, qindex, dequant, scan, nil, dqcoeff)
}

// QuantizeBWithQ mirrors libvpx v1.16.0 vpx_quantize_b_c for 4x4/8x8/16x16
// transforms and additionally writes the signed quantized coefficients into
// qcoeff[] in raster order. libvpx's cost_coeffs (vp9_rdopt.c:367,392,405)
// reads qcoeff directly; recovering q from dqcoeff via division loses
// precision whenever q*dequant overflows int16 (the implicit int16 cast
// in dqcoeff[rc] = int16(q * dq) wraps), so callers in the cost-coeffs
// chain must consume qcoeff to stay byte-identical with libvpx.
// libvpx: vpx_dsp/quantize.c:42-77 vpx_quantize_b_c (qcoeff_ptr + dqcoeff_ptr).
// qcoeff may be nil when the caller only needs dqcoeff (legacy bitstream-
// emit paths that immediately inverse-transform from dqcoeff).
func QuantizeBWithQ(coeff []int16, qindex int, dequant [2]int16, scan []int16,
	qcoeff, dqcoeff []int16,
) int {
	n := min(len(coeff), min(len(scan), len(dqcoeff)))
	if qcoeff != nil && len(qcoeff) < n {
		n = len(qcoeff)
	}
	for i := range n {
		dqcoeff[i] = 0
		if qcoeff != nil {
			qcoeff[i] = 0
		}
	}
	if n == 0 || dequant[0] == 0 || dequant[1] == 0 {
		return 0
	}

	params := vp9QuantizeBParams(qindex, dequant)
	nonZeroCount := n
	for i := n - 1; i >= 0; i-- {
		rc := int(scan[i])
		slot := vp9CoeffQuantSlot(rc)
		c := int(coeff[rc])
		if c < params.zbin[slot] && c > -params.zbin[slot] {
			nonZeroCount--
			continue
		}
		break
	}

	eob := -1
	for i := 0; i < nonZeroCount; i++ {
		rc := int(scan[i])
		slot := vp9CoeffQuantSlot(rc)
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		if absCoeff < params.zbin[slot] {
			continue
		}
		tmp := clampInt16(absCoeff + params.round[slot])
		q := ((((tmp * params.quant[slot]) >> 16) + tmp) *
			params.quantShift[slot]) >> 16
		if c < 0 {
			q = -q
		}
		// libvpx vpx_dsp/quantize.c:71-72:
		//   qcoeff_ptr[rc] = (tmp ^ coeff_sign) - coeff_sign;
		//   dqcoeff_ptr[rc] = qcoeff_ptr[rc] * dequant_ptr[rc != 0];
		if qcoeff != nil {
			qcoeff[rc] = int16(q)
		}
		dqcoeff[rc] = int16(q * int(dequant[slot]))
		if q != 0 {
			eob = i
		}
	}
	return eob + 1
}

// QuantizeB32x32 mirrors libvpx v1.16.0 vpx_quantize_b_32x32_c.
func QuantizeB32x32(coeff []int16, qindex int, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	return QuantizeB32x32WithQ(coeff, qindex, dequant, scan, nil, dqcoeff)
}

// QuantizeB32x32WithQ mirrors libvpx v1.16.0 vpx_quantize_b_32x32_c and
// additionally writes the signed quantized coefficients into qcoeff[] in
// raster order. See QuantizeBWithQ for the rationale — libvpx's cost_coeffs
// reads qcoeff directly because the Tx32x32 dequant rule
//
//	dqcoeff[rc] = qcoeff[rc] * dequant[rc != 0] / 2
//
// (vpx_dsp/quantize.c:269) only fits in int16 for |q*dq/2| <= 32767, so
// dqcoeff can wrap whenever q*dq exceeds 65534 (e.g. dq=1828, |q|>=36).
// Recovering q from int16-wrapped dqcoeff drifts in high-frequency Tx32x32
// bands, so callers that score coefficients need the signed qcoeff values.
func QuantizeB32x32WithQ(coeff []int16, qindex int, dequant [2]int16, scan []int16,
	qcoeff, dqcoeff []int16,
) int {
	const nCoeffs = 32 * 32
	n := min(nCoeffs, min(len(coeff), min(len(scan), len(dqcoeff))))
	if qcoeff != nil && len(qcoeff) < n {
		n = len(qcoeff)
	}
	for i := range n {
		dqcoeff[i] = 0
		if qcoeff != nil {
			qcoeff[i] = 0
		}
	}
	if n == 0 || dequant[0] == 0 || dequant[1] == 0 {
		return 0
	}

	params := vp9QuantizeBParams(qindex, dequant)
	zbin := [2]int{
		vp9RoundPowerOfTwo(params.zbin[0], 1),
		vp9RoundPowerOfTwo(params.zbin[1], 1),
	}
	eob := -1
	for scanIdx := range n {
		rc := int(scan[scanIdx])
		slot := vp9CoeffQuantSlot(rc)
		c := int(coeff[rc])
		if c < zbin[slot] && c > -zbin[slot] {
			continue
		}
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		absCoeff = clampInt16(absCoeff + vp9RoundPowerOfTwo(params.round[slot], 1))
		q := ((((absCoeff * params.quant[slot]) >> 16) + absCoeff) *
			params.quantShift[slot]) >> 15
		if c < 0 {
			q = -q
		}
		// libvpx vpx_dsp/quantize.c:261,269:
		//   qcoeff_ptr[rc] = (tmp ^ coeff_sign) - coeff_sign;
		//   dqcoeff_ptr[rc] = qcoeff_ptr[rc] * dequant_ptr[rc != 0] / 2;
		if qcoeff != nil {
			qcoeff[rc] = int16(q)
		}
		dqcoeff[rc] = int16(q * int(dequant[slot]) / 2)
		if q != 0 {
			eob = scanIdx
		}
	}
	return eob + 1
}

type vp9QuantizeParams struct {
	zbin       [2]int
	round      [2]int
	quant      [2]int
	quantShift [2]int
}

func vp9QuantizeBParams(qindex int, dequant [2]int16) vp9QuantizeParams {
	qroundingFactor := 48
	if qindex == 0 {
		qroundingFactor = 64
	}
	qzbinFactor := vp9QzbinFactor(qindex)
	var params vp9QuantizeParams
	for i := range 2 {
		dq := int(dequant[i])
		params.zbin[i] = vp9RoundPowerOfTwo(qzbinFactor*dq, 7)
		params.round[i] = (qroundingFactor * dq) >> 7
		params.quant[i], params.quantShift[i] = vp9InvertQuant(dq)
	}
	return params
}

func vp9QzbinFactor(qindex int) int {
	if qindex == 0 {
		return 64
	}
	if int(common.DcQuant(qindex, 0, common.Bits8)) < 148 {
		return 84
	}
	return 80
}

func vp9InvertQuant(d int) (quant, shift int) {
	if d <= 0 {
		return 0, 0
	}
	l := bits.Len(uint(d)) - 1
	m := 1 + (1 << uint(16+l) / d)
	return int(int16(m - (1 << 16))), 1 << uint(16-l)
}

func vp9CoeffQuantSlot(rc int) int {
	if rc == 0 {
		return 0
	}
	return 1
}

func vp9RoundPowerOfTwo(v, n int) int {
	if n <= 0 {
		return v
	}
	return (v + (1 << uint(n-1))) >> uint(n)
}

// QuantizeFP mirrors libvpx's vp9_quantize_fp_c for non-32x32 transforms.
// dqcoeff receives dequantized coefficients in raster order, which is the
// representation consumed by WriteCoefBlock. The return value is the scan-order
// EOB position. Dispatches to a SIMD kernel when available.
//
// This is the legacy entry point: round_fp/quant_fp are derived from
// dequant using the same recipe vp9_init_quantizer uses
// (libvpx: vp9/encoder/vp9_quantize.c:209-210), and qcoeff/iscan are
// synthesised on the fly. Callers that already hold round_fp/quant_fp +
// iscan should use QuantizeFPLibvpx directly to avoid the conversion and
// to share scratch with the NEON kernel verbatim.
func QuantizeFP(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	return quantizeFPDispatch(coeff, dequant, scan, dqcoeff)
}

// QuantizeFPWithQ mirrors libvpx's vp9_quantize_fp_c for non-32x32 transforms
// and additionally writes the signed quantized coefficients into qcoeff[] in
// raster order. See QuantizeBWithQ for the cost_coeffs rationale.
// libvpx: vp9/encoder/vp9_quantize.c:26-56 vp9_quantize_fp_c (qcoeff_ptr +
// dqcoeff_ptr both written from the same tmp/sign pair).
func QuantizeFPWithQ(coeff []int16, dequant [2]int16, scan []int16,
	qcoeff, dqcoeff []int16,
) int {
	n := min(len(coeff), min(len(scan), len(dqcoeff)))
	if qcoeff != nil && len(qcoeff) < n {
		n = len(qcoeff)
	}
	if n == 0 {
		return 0
	}
	// libvpx: vp9/encoder/vp9_quantize.c:209-210 — derive fp tables from
	// dequant the same way vp9_init_quantizer does:
	//   quants->y_quant_fp[q][i] = (1 << 16) / quant;
	//   quants->y_round_fp[q][i] = (qrounding_factor_fp * quant) >> 7;
	// where qrounding_factor_fp = i == 0 ? 48 : 42 (non-q0, no sharpness).
	roundFP := [2]int16{
		int16((48 * int(dequant[0])) >> 7),
		int16((42 * int(dequant[1])) >> 7),
	}
	quantFP := [2]int16{
		int16((1 << 16) / int(dequant[0])),
		int16((1 << 16) / int(dequant[1])),
	}
	var iscanBuf [1024]int16
	iscan := iscanBuf[:n]
	for i := range n {
		rc := int(scan[i])
		if rc >= 0 && rc < n {
			iscan[rc] = int16(i + 1)
		}
	}
	return quantizeFPWithQTables(coeff[:n], dequant, roundFP, quantFP,
		scan[:n], iscan, qcoeff, dqcoeff[:n])
}

// QuantizeFPWithQScanOrder is QuantizeFPWithQ for callers that already hold
// the libvpx ScanOrder and can reuse its precomputed inverse scan.
func QuantizeFPWithQScanOrder(coeff []int16, dequant [2]int16, scanOrder common.ScanOrder,
	qcoeff, dqcoeff []int16,
) int {
	n := min(len(coeff), min(len(scanOrder.Scan), min(len(scanOrder.IScan), len(dqcoeff))))
	if qcoeff != nil && len(qcoeff) < n {
		n = len(qcoeff)
	}
	if n == 0 {
		return 0
	}
	roundFP := [2]int16{
		int16((48 * int(dequant[0])) >> 7),
		int16((42 * int(dequant[1])) >> 7),
	}
	quantFP := [2]int16{
		int16((1 << 16) / int(dequant[0])),
		int16((1 << 16) / int(dequant[1])),
	}
	return quantizeFPWithQTables(coeff[:n], dequant, roundFP, quantFP,
		scanOrder.Scan[:n], scanOrder.IScan[:n], qcoeff, dqcoeff[:n])
}

func quantizeFPWithQTables(coeff []int16, dequant, roundFP, quantFP [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	n := len(coeff)
	if qcoeff == nil {
		var qcoeffBuf [1024]int16
		qcoeff = qcoeffBuf[:n]
		return quantizeFPLibvpxScalar(coeff, n, roundFP, quantFP, dequant,
			scan, iscan, qcoeff, dqcoeff)
	}
	return quantizeFPLibvpxScalar(coeff, n, roundFP, quantFP, dequant,
		scan, iscan, qcoeff[:n], dqcoeff)
}

// QuantizeFPLibvpx is the verbatim Go entry point matching libvpx's
// vp9_quantize_fp signature so the NEON kernel (vp9_quantize_fp_neon) can
// plug in byte-identically. Inputs/outputs follow the libvpx contract:
//
//   - coeff[0..nCoeffs)   : raster-order DCT output
//   - roundFP[2]          : mb_plane->round_fp [DC, AC]
//   - quantFP[2]          : mb_plane->quant_fp [DC, AC] = (1<<16)/dequant
//   - dequant[2]          : per-plane dequant [DC, AC]
//   - scan, iscan         : ScanOrder tables; scan[i] -> raster pos,
//     iscan[rc] = scan_pos(rc) + 1
//   - qcoeff[0..nCoeffs)  : OUT quantized coefficients, raster order
//   - dqcoeff[0..nCoeffs) : OUT dequantized coefficients, raster order
//
// Returns the 1-indexed EOB position (== max(iscan[rc] | qcoeff[rc] != 0)).
// libvpx: vp9/encoder/vp9_quantize.c:26 vp9_quantize_fp_c
func QuantizeFPLibvpx(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	return quantizeFPLibvpxDispatch(coeff, nCoeffs, roundFP, quantFP, dequant,
		scan, iscan, qcoeff, dqcoeff)
}

// quantizeFPLibvpxScalar is the verbatim Go port of libvpx v1.16.0
// vp9_quantize_fp_c. The signature mirrors the libvpx kernel contract so
// vp9_quantize_fp_neon can swap in byte-identically: split qcoeff/dqcoeff
// outputs in raster order, separate round_fp/quant_fp/dequant tables, and
// scan-order eob tracking.
// libvpx: vp9/encoder/vp9_quantize.c:26-56 vp9_quantize_fp_c
func quantizeFPLibvpxScalar(coeff []int16, nCoeffs int, roundFP, quantFP, dequant [2]int16,
	scan, iscan []int16, qcoeff, dqcoeff []int16,
) int {
	// libvpx: vp9/encoder/vp9_quantize.c:32-34 (round_ptr/quant_ptr/scan)
	roundDC, roundAC := int(roundFP[0]), int(roundFP[1])
	quantDC, quantAC := int(quantFP[0]), int(quantFP[1])
	deqDC, deqAC := int(dequant[0]), int(dequant[1])
	if len(iscan) >= nCoeffs {
		// Match libvpx's SIMD shape: quantize raster-order coefficients and
		// max-reduce iscan for EOB. Valid VP9 scan orders make this equivalent
		// to the C path's scan-order loop.
		eob := 0
		if nCoeffs > 0 {
			c := int(coeff[0])
			absCoeff := c
			if absCoeff < 0 {
				absCoeff = -absCoeff
			}
			sum := absCoeff + roundDC
			if sum >= deqDC {
				tmp := clampInt16(sum)
				tmp = (tmp * quantDC) >> 16
				q := tmp
				if c < 0 {
					q = -q
				}
				qcoeff[0] = int16(q)
				dqcoeff[0] = int16(q * deqDC)
				if tmp != 0 {
					eob = int(iscan[0])
				}
			} else {
				qcoeff[0] = 0
				dqcoeff[0] = 0
			}
		}
		for rc := 1; rc < nCoeffs; rc++ {
			c := int(coeff[rc])
			absCoeff := c
			if absCoeff < 0 {
				absCoeff = -absCoeff
			}
			sum := absCoeff + roundAC
			if sum < deqAC {
				qcoeff[rc] = 0
				dqcoeff[rc] = 0
				continue
			}
			tmp := clampInt16(sum)
			tmp = (tmp * quantAC) >> 16
			q := tmp
			if c < 0 {
				q = -q
			}
			qcoeff[rc] = int16(q)
			dqcoeff[rc] = int16(q * deqAC)
			if tmp != 0 && int(iscan[rc]) > eob {
				eob = int(iscan[rc])
			}
		}
		return eob
	}
	// libvpx: vp9/encoder/vp9_quantize.c:36-37
	//   memset(qcoeff_ptr, 0, n_coeffs * sizeof(*qcoeff_ptr));
	//   memset(dqcoeff_ptr, 0, n_coeffs * sizeof(*dqcoeff_ptr));
	clear(qcoeff[:nCoeffs])
	clear(dqcoeff[:nCoeffs])
	// libvpx: vp9/encoder/vp9_quantize.c:31 (int i, eob = -1)
	// The scalar C path sets eob to the current scan index. SIMD kernels
	// instead max-reduce iscan[rc], which is equivalent for valid VP9 scan
	// orders because iscan[scan[i]] == i + 1.
	eob := -1
	for i := range nCoeffs {
		// libvpx: vp9/encoder/vp9_quantize.c:42-45
		rc := int(scan[i])
		round, quant, deq := roundAC, quantAC, deqAC
		if rc == 0 {
			round, quant, deq = roundDC, quantDC, deqDC
		}
		c := int(coeff[rc])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		sum := absCoeff + round
		if sum < deq {
			continue
		}
		// libvpx: vp9/encoder/vp9_quantize.c:47-48
		//   tmp = clamp(abs_coeff + round_ptr[rc != 0], INT16_MIN, INT16_MAX);
		//   tmp = (tmp * quant_ptr[rc != 0]) >> 16;
		tmp := clampInt16(sum)
		tmp = (tmp * quant) >> 16
		// libvpx: vp9/encoder/vp9_quantize.c:50-51
		//   qcoeff_ptr[rc] = (tmp ^ coeff_sign) - coeff_sign;
		//   dqcoeff_ptr[rc] = qcoeff_ptr[rc] * dequant_ptr[rc != 0];
		q := tmp
		if c < 0 {
			q = -q
		}
		qcoeff[rc] = int16(q)
		dqcoeff[rc] = int16(q * deq)
		// libvpx: vp9/encoder/vp9_quantize.c:53 (if (tmp) eob = i;)
		if tmp != 0 {
			eob = i
		}
	}
	// libvpx: vp9/encoder/vp9_quantize.c:55 (*eob_ptr = eob + 1;)
	return eob + 1
}

// quantizeFPScalar is the canonical scalar port of libvpx's
// vp9_quantize_fp_c packaged behind the legacy QuantizeFP signature
// (dqcoeff-only, dequant-derived round/quant, scan only).  It allocates a
// stack-bounded qcoeff scratch and derives the iscan table internally,
// then funnels through quantizeFPLibvpxScalar so the byte-identical
// libvpx contract holds end-to-end. SIMD implementations must produce
// byte-identical dqcoeff and eob values.
func quantizeFPScalar(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	n := min(len(coeff), min(len(scan), len(dqcoeff)))
	if n == 0 {
		return 0
	}
	// libvpx: vp9/encoder/vp9_quantize.c:209-210 — derive fp tables
	// from dequant the same way vp9_init_quantizer does:
	//   quants->y_quant_fp[q][i] = (1 << 16) / quant;
	//   quants->y_round_fp[q][i] = (qrounding_factor_fp * quant) >> 7;
	// where qrounding_factor_fp = i == 0 ? 48 : 42 (non-q0, no sharpness).
	roundFP := [2]int16{
		int16((48 * int(dequant[0])) >> 7),
		int16((42 * int(dequant[1])) >> 7),
	}
	quantFP := [2]int16{
		int16((1 << 16) / int(dequant[0])),
		int16((1 << 16) / int(dequant[1])),
	}
	// Synthesise iscan on the fly: iscan[scan[i]] = i + 1. Allocates a
	// 1024-entry scratch (max TX block area) on the stack-resident
	// fixed-size array so the legacy path stays alloc-free.
	var iscanBuf [1024]int16
	iscan := iscanBuf[:n]
	var qcoeffBuf [1024]int16
	qcoeff := qcoeffBuf[:n]
	for i := range n {
		rc := int(scan[i])
		if rc >= 0 && rc < n {
			iscan[rc] = int16(i + 1)
		}
	}
	return quantizeFPLibvpxScalar(coeff, n, roundFP, quantFP, dequant,
		scan, iscan, qcoeff, dqcoeff)
}

// QuantizeFP4x4 mirrors libvpx's vp9_quantize_fp_c for a 4x4 transform.
func QuantizeFP4x4(coeff *[16]int16, dequant [2]int16, scan []int16, dqcoeff *[16]int16) int {
	return QuantizeFP(coeff[:], dequant, scan, dqcoeff[:])
}

// QuantizeFP32x32 mirrors libvpx's vp9_quantize_fp_32x32_c.
func QuantizeFP32x32(coeff []int16, dequant [2]int16, scan []int16, dqcoeff []int16) int {
	return QuantizeFP32x32WithQ(coeff, dequant, scan, nil, dqcoeff)
}

// QuantizeFP32x32WithQ mirrors libvpx's vp9_quantize_fp_32x32_c and
// additionally writes the signed quantized coefficients into qcoeff[] in
// raster order. See QuantizeBWithQ / QuantizeB32x32WithQ for the
// cost_coeffs rationale (libvpx vp9_rdopt.c:367,405 reads qcoeff directly;
// int16-wrapped dqcoeff loses information when q*dq overflows).
// libvpx: vp9/encoder/vp9_quantize.c:92-123 vp9_quantize_fp_32x32_c.
func QuantizeFP32x32WithQ(coeff []int16, dequant [2]int16, scan []int16,
	qcoeff, dqcoeff []int16,
) int {
	quant := [2]int{(1 << 16) / int(dequant[0]), (1 << 16) / int(dequant[1])}
	round := [2]int{
		(((48 * int(dequant[0])) >> 7) + 1) >> 1,
		(((42 * int(dequant[1])) >> 7) + 1) >> 1,
	}
	n := min(len(coeff), min(len(scan), len(dqcoeff)))
	if qcoeff != nil && len(qcoeff) < n {
		n = len(qcoeff)
	}
	if n == 0 {
		return 0
	}
	// libvpx's 32x32 VP9 transforms only use the default DCT_DCT scan, so
	// the hot path can mirror SIMD kernels: raster-order quantization plus
	// max-reduced iscan for EOB.
	if n == 1024 && isDefaultVP9Scan32x32(scan) {
		return quantizeFP32x32Raster(coeff[:n], dequant, quant, round,
			common.DefaultScanOrders[common.Tx32x32].IScan[:n], qcoeff, dqcoeff[:n])
	}
	if qcoeff == nil {
		return quantizeFP32x32Scan(coeff[:n], dequant, quant, round,
			scan[:n], nil, dqcoeff[:n])
	}
	return quantizeFP32x32Scan(coeff[:n], dequant, quant, round,
		scan[:n], qcoeff[:n], dqcoeff[:n])
}

func isDefaultVP9Scan32x32(scan []int16) bool {
	if len(scan) < 1024 {
		return false
	}
	defaultScan := common.DefaultScanOrders[common.Tx32x32].Scan
	return len(defaultScan) >= 1024 && &scan[0] == &defaultScan[0]
}

func quantizeFP32x32Raster(coeff []int16, dequant [2]int16, quant, round [2]int,
	iscan []int16, qcoeff, dqcoeff []int16,
) int {
	deqDC, deqAC := int(dequant[0]), int(dequant[1])
	quantDC, quantAC := quant[0], quant[1]
	roundDC, roundAC := round[0], round[1]
	eob := 0

	if qcoeff == nil {
		c := int(coeff[0])
		absCoeff := c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		tmp := 0
		if absCoeff >= deqDC>>2 {
			tmp = clampInt16(absCoeff + roundDC)
			tmp = (tmp * quantDC) >> 15
			q := tmp
			if c < 0 {
				q = -q
			}
			dqcoeff[0] = int16(q * deqDC / 2)
		} else {
			dqcoeff[0] = 0
		}
		if tmp != 0 {
			eob = int(iscan[0])
		}

		for rc := 1; rc < 1024; rc++ {
			c = int(coeff[rc])
			absCoeff = c
			if absCoeff < 0 {
				absCoeff = -absCoeff
			}
			tmp = 0
			if absCoeff >= deqAC>>2 {
				tmp = clampInt16(absCoeff + roundAC)
				tmp = (tmp * quantAC) >> 15
				q := tmp
				if c < 0 {
					q = -q
				}
				dqcoeff[rc] = int16(q * deqAC / 2)
			} else {
				dqcoeff[rc] = 0
			}
			if tmp != 0 && int(iscan[rc]) > eob {
				eob = int(iscan[rc])
			}
		}
		return eob
	}

	c := int(coeff[0])
	absCoeff := c
	if absCoeff < 0 {
		absCoeff = -absCoeff
	}
	tmp := 0
	if absCoeff >= deqDC>>2 {
		tmp = clampInt16(absCoeff + roundDC)
		tmp = (tmp * quantDC) >> 15
		q := tmp
		if c < 0 {
			q = -q
		}
		qcoeff[0] = int16(q)
		dqcoeff[0] = int16(q * deqDC / 2)
	} else {
		qcoeff[0] = 0
		dqcoeff[0] = 0
	}
	if tmp != 0 {
		eob = int(iscan[0])
	}

	for rc := 1; rc < 1024; rc++ {
		c = int(coeff[rc])
		absCoeff = c
		if absCoeff < 0 {
			absCoeff = -absCoeff
		}
		tmp = 0
		if absCoeff >= deqAC>>2 {
			tmp = clampInt16(absCoeff + roundAC)
			tmp = (tmp * quantAC) >> 15
			q := tmp
			if c < 0 {
				q = -q
			}
			qcoeff[rc] = int16(q)
			dqcoeff[rc] = int16(q * deqAC / 2)
		} else {
			qcoeff[rc] = 0
			dqcoeff[rc] = 0
		}
		if tmp != 0 && int(iscan[rc]) > eob {
			eob = int(iscan[rc])
		}
	}
	return eob
}

func quantizeFP32x32Scan(coeff []int16, dequant [2]int16, quant, round [2]int,
	scan []int16, qcoeff, dqcoeff []int16,
) int {
	n := len(coeff)
	eob := -1
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
			// libvpx vp9/encoder/vp9_quantize.c:116-117:
			//   qcoeff_ptr[rc] = (tmp ^ coeff_sign) - coeff_sign;
			//   dqcoeff_ptr[rc] = qcoeff_ptr[rc] * dequant_ptr[rc != 0] / 2;
			if qcoeff != nil {
				qcoeff[rc] = int16(q)
			}
			dqcoeff[rc] = int16(q * int(dequant[slot]) / 2)
		} else {
			if qcoeff != nil {
				qcoeff[rc] = 0
			}
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

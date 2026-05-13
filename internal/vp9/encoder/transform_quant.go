package encoder

const (
	fdctDctConstBits     = 14
	fdctDctConstRounding = 1 << (fdctDctConstBits - 1)

	fdctCospi4_64  = 16069
	fdctCospi8_64  = 15137
	fdctCospi12_64 = 13623
	fdctCospi16_64 = 11585
	fdctCospi20_64 = 9102
	fdctCospi24_64 = 6270
	fdctCospi28_64 = 3196
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

func fdctRoundShift(input int) int {
	return (input + fdctDctConstRounding) >> fdctDctConstBits
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

func clampInt16(v int) int {
	if v > 32767 {
		return 32767
	}
	return v
}

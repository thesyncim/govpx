package dsp

// Ported from libvpx v1.16.0 vp8/common/idctllm.c.

const (
	cosPI8Sqrt2Minus1 = 20091
	sinPI8Sqrt2       = 35468
)

// IDCT4x4Add dispatches to the SIMD or scalar 4x4 inverse DCT-add kernel.
func IDCT4x4Add(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	idct4x4AddSIMD(input, pred, predStride, dst, dstStride)
}

// DCOnlyIDCT4x4Add dispatches to the SIMD or scalar DC-only fast path.
func DCOnlyIDCT4x4Add(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	dcOnlyIDCT4x4AddSIMD(inputDC, pred, predStride, dst, dstStride)
}

// idct4x4AddScalar is the canonical scalar port of libvpx
// vp8/common/idctllm.c vp8_short_idct4x4llm_c. SIMD ports must produce
// byte-identical output for the encoder/decoder coefficient range.
func idct4x4AddScalar(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	var output [16]int16

	for i := 0; i < 4; i++ {
		a1 := int(input[i+0]) + int(input[i+8])
		b1 := int(input[i+0]) - int(input[i+8])

		temp1 := (int(input[i+4]) * sinPI8Sqrt2) >> 16
		temp2 := int(input[i+12]) + ((int(input[i+12]) * cosPI8Sqrt2Minus1) >> 16)
		c1 := temp1 - temp2

		temp1 = int(input[i+4]) + ((int(input[i+4]) * cosPI8Sqrt2Minus1) >> 16)
		temp2 = (int(input[i+12]) * sinPI8Sqrt2) >> 16
		d1 := temp1 + temp2

		output[i+0] = int16(a1 + d1)
		output[i+12] = int16(a1 - d1)
		output[i+4] = int16(b1 + c1)
		output[i+8] = int16(b1 - c1)
	}

	for i := 0; i < 4; i++ {
		base := i * 4
		a1 := int(output[base+0]) + int(output[base+2])
		b1 := int(output[base+0]) - int(output[base+2])

		temp1 := (int(output[base+1]) * sinPI8Sqrt2) >> 16
		temp2 := int(output[base+3]) + ((int(output[base+3]) * cosPI8Sqrt2Minus1) >> 16)
		c1 := temp1 - temp2

		temp1 = int(output[base+1]) + ((int(output[base+1]) * cosPI8Sqrt2Minus1) >> 16)
		temp2 = (int(output[base+3]) * sinPI8Sqrt2) >> 16
		d1 := temp1 + temp2

		output[base+0] = int16((a1 + d1 + 4) >> 3)
		output[base+3] = int16((a1 - d1 + 4) >> 3)
		output[base+1] = int16((b1 + c1 + 4) >> 3)
		output[base+2] = int16((b1 - c1 + 4) >> 3)
	}

	for y := 0; y < 4; y++ {
		predRow := y * predStride
		dstRow := y * dstStride
		outRow := y * 4
		dst[dstRow+0] = ClipPixel(int(output[outRow+0]) + int(pred[predRow+0]))
		dst[dstRow+1] = ClipPixel(int(output[outRow+1]) + int(pred[predRow+1]))
		dst[dstRow+2] = ClipPixel(int(output[outRow+2]) + int(pred[predRow+2]))
		dst[dstRow+3] = ClipPixel(int(output[outRow+3]) + int(pred[predRow+3]))
	}
}

// dcOnlyIDCT4x4AddScalar is the canonical scalar fast path used when only
// the DC coefficient is non-zero.
func dcOnlyIDCT4x4AddScalar(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	a1 := int((inputDC + 4) >> 3)
	for y := 0; y < 4; y++ {
		predRow := y * predStride
		dstRow := y * dstStride
		dst[dstRow+0] = ClipPixel(a1 + int(pred[predRow+0]))
		dst[dstRow+1] = ClipPixel(a1 + int(pred[predRow+1]))
		dst[dstRow+2] = ClipPixel(a1 + int(pred[predRow+2]))
		dst[dstRow+3] = ClipPixel(a1 + int(pred[predRow+3]))
	}
}

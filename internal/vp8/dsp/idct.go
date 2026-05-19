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

// DCOnlyIDCT4x4AddInt32 is the int32-precision DC-only fast path used by the
// decoder DC-only chroma/luma residual adds when QCoeff * Dequant may overflow
// the int16 range. It mirrors libvpx v1.16.0
// vp8/common/arm/neon/idct_blk_neon.c idct_dequant_0_2x_neon, which performs
// the multiply-shift in int precision before broadcasting the result to a
// signed-16-bit NEON vector. The narrow-to-int16 happens after the (>>3)
// shift, which keeps the lane value in a far smaller magnitude than the raw
// product, so a coefficient like 334 * 132 = 44088 does NOT wrap.
//
// The non-NEON scalar libvpx path DOES wrap (it passes the raw product
// through a `short input_dc` argument), but vpxdec on arm64 dispatches to
// the NEON variant, so byte-exact parity requires mirroring the NEON
// semantics here.
func DCOnlyIDCT4x4AddInt32(inputDC int32, pred []byte, predStride int, dst []byte, dstStride int) {
	a1 := int((inputDC + 4) >> 3)
	for y := range 4 {
		dstRow := dst[y*dstStride : y*dstStride+4 : y*dstStride+4]
		predRow := pred[y*predStride : y*predStride+4 : y*predStride+4]
		dstRow[0] = ClipPixel(a1 + int(predRow[0]))
		dstRow[1] = ClipPixel(a1 + int(predRow[1]))
		dstRow[2] = ClipPixel(a1 + int(predRow[2]))
		dstRow[3] = ClipPixel(a1 + int(predRow[3]))
	}
}

// idct4x4AddScalar is the canonical scalar port of libvpx
// vp8/common/idctllm.c vp8_short_idct4x4llm_c. SIMD ports must produce
// byte-identical output for the encoder/decoder coefficient range.
func idct4x4AddScalar(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	var output [16]int16

	for i := range 4 {
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

	for i := range 4 {
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

	for y := range 4 {
		// Reslice to a 4-element row so the four writes share a single
		// bounds check instead of one per ClipPixel call.
		dstRow := dst[y*dstStride : y*dstStride+4 : y*dstStride+4]
		predRow := pred[y*predStride : y*predStride+4 : y*predStride+4]
		outRow := y * 4
		dstRow[0] = ClipPixel(int(output[outRow+0]) + int(predRow[0]))
		dstRow[1] = ClipPixel(int(output[outRow+1]) + int(predRow[1]))
		dstRow[2] = ClipPixel(int(output[outRow+2]) + int(predRow[2]))
		dstRow[3] = ClipPixel(int(output[outRow+3]) + int(predRow[3]))
	}
}

// dcOnlyIDCT4x4AddScalar is the canonical scalar fast path used when only
// the DC coefficient is non-zero.
func dcOnlyIDCT4x4AddScalar(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	a1 := int((inputDC + 4) >> 3)
	for y := range 4 {
		dstRow := dst[y*dstStride : y*dstStride+4 : y*dstStride+4]
		predRow := pred[y*predStride : y*predStride+4 : y*predStride+4]
		dstRow[0] = ClipPixel(a1 + int(predRow[0]))
		dstRow[1] = ClipPixel(a1 + int(predRow[1]))
		dstRow[2] = ClipPixel(a1 + int(predRow[2]))
		dstRow[3] = ClipPixel(a1 + int(predRow[3]))
	}
}

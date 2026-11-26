package dsp

import "github.com/thesyncim/govpx/internal/pixel"

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
	if uint16(inputDC)+4 <= 7 && zeroDCSameWindow(pred, predStride, dst, dstStride) {
		return
	}

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
	a1 := dcOnlyIDCT4x4AddInt32Delta(inputDC)
	if a1 == 0 && zeroDCSameWindow(pred, predStride, dst, dstStride) {
		return
	}
	dcOnlyIDCT4x4AddSIMD(int16(a1<<3), pred, predStride, dst, dstStride)
}

// DCOnlyIDCT4x4AddPairInPlaceInt32 mirrors libvpx v1.16.0's paired DC-only
// reconstruction when prediction and destination are the same full window.
func DCOnlyIDCT4x4AddPairInPlaceInt32(c0, c1 int32, b []byte, stride int) {
	if pixel.DCPairNative && pixel.Enabled {
		pixel.DCPairInPlaceInt32Native(c0, c1, b, stride)
		return
	}
	DCOnlyIDCT4x4AddPairInt32(c0, c1, b, stride, b, stride)
}

// DCOnlyIDCT4x4AddPairInt32 adds two horizontally adjacent DC-only 4x4 blocks.
// It is used by chroma residual paths that mirror libvpx's paired U/V block
// dispatch.
func DCOnlyIDCT4x4AddPairInt32(inputDC0 int32, inputDC1 int32, pred []byte, predStride int, dst []byte, dstStride int) {
	if pixel.DCPairNative && pixel.Enabled {
		pixel.DCPairInt32Native(inputDC0, inputDC1, pred, predStride, dst, dstStride)
		return
	}
	a0 := dcOnlyIDCT4x4AddInt32Delta(inputDC0)
	a1 := dcOnlyIDCT4x4AddInt32Delta(inputDC1)
	dcOnlyIDCT4x4AddPairSIMD(int16(a0), int16(a1), pred, predStride, dst, dstStride)
}

// DequantIDCTAddFull2x mirrors libvpx v1.16.0
// vp8/common/arm/neon/idct_blk_neon.c idct_dequant_full_2x_neon: a
// horizontally adjacent pair of 4x4 blocks is dequantized with plain
// int16 (wrapping) multiplies across all 16 positions and inverse
// transformed, adding onto the prediction already present in dst (left
// block) and dst[4:] (right block). Unlike libvpx the coefficients are
// not cleared here; the decoder's token dirty mask handles that.
//
// q holds the two blocks contiguously (32 int16). dst must cover
// 4 rows of 8 bytes (len >= 3*stride+8).
func DequantIDCTAddFull2x(q *[32]int16, dq *[16]int16, dst []byte, stride int) {
	dequantIDCTAddFull2xSIMD(q, dq, dst, stride)
}

// DequantIDCTAddFull2xBlocks runs fused dequant + 4x4 IDCT + saturating add
// directly from two caller-owned 16-int16 blocks without staging into a
// contiguous 32-int16 scratch buffer.
func DequantIDCTAddFull2xBlocks(q0, q1 *[16]int16, dq *[16]int16, dst []byte, stride int) {
	if dequantInverse4BlocksGo(q0, q1, dq, dst, stride) {
		return
	}
	var tmp [16]int16
	for i := range 16 {
		tmp[i] = q0[i] * dq[i]
	}
	IDCT4x4Add(&tmp, dst, stride, dst, stride)
	for i := range 16 {
		tmp[i] = q1[i] * dq[i]
	}
	IDCT4x4Add(&tmp, dst[4:], stride, dst[4:], stride)
}

// DequantIDCTAddFull1x runs fused dequant + 4x4 IDCT + saturating add on a
// single 4x4 block in place.
func DequantIDCTAddFull1x(q *[16]int16, dq *[16]int16, dst []byte, stride int) {
	if dequantInverse4SingleGo(q, dq, dst, stride) {
		return
	}
	var tmp [16]int16
	for i := range 16 {
		tmp[i] = q[i] * dq[i]
	}
	IDCT4x4Add(&tmp, dst, stride, dst, stride)
}

// dequantIDCTAddFull2xFallback composes the fused pair kernel from the
// per-block primitives: full 16-position wrapping dequant followed by
// the standard inverse transform. Matches the NEON pair kernel for VP8
// coefficient ranges (same contract as the other scalar fallbacks).
func dequantIDCTAddFull2xFallback(q *[32]int16, dq *[16]int16, dst []byte, stride int) {
	var tmp [16]int16
	for i := range 16 {
		tmp[i] = q[i] * dq[i]
	}
	IDCT4x4Add(&tmp, dst, stride, dst, stride)
	for i := range 16 {
		tmp[i] = q[16+i] * dq[i]
	}
	IDCT4x4Add(&tmp, dst[4:], stride, dst[4:], stride)
}

func dcOnlyIDCT4x4AddInt32Delta(inputDC int32) int {
	a1 := int((inputDC + 4) >> 3)
	if a1 > 255 {
		a1 = 255
	} else if a1 < -255 {
		a1 = -255
	}
	return a1
}

// idct4x4AddScalar is the canonical scalar port of libvpx
// vp8/common/idctllm.c vp8_short_idct4x4llm_c. SIMD ports must produce
// byte-identical output for the encoder/decoder coefficient range.
func idct4x4AddScalar(input *[16]int16, pred []byte, predStride int, dst []byte, dstStride int) {
	var output [16]int16

	for i := range 4 {
		i0, i4, i8, i12 := int(input[i]), int(input[i+4]), int(input[i+8]), int(input[i+12])
		if i0|i4|i8|i12 == 0 {
			continue
		}
		a1 := i0 + i8
		b1 := i0 - i8

		c1 := ((i4 * sinPI8Sqrt2) >> 16) - (i12 + ((i12 * cosPI8Sqrt2Minus1) >> 16))
		d1 := (i4 + ((i4 * cosPI8Sqrt2Minus1) >> 16)) + ((i12 * sinPI8Sqrt2) >> 16)

		output[i+0] = int16(a1 + d1)
		output[i+12] = int16(a1 - d1)
		output[i+4] = int16(b1 + c1)
		output[i+8] = int16(b1 - c1)
	}

	for y := range 4 {
		base := y * 4
		o0, o1, o2, o3 := int(output[base+0]), int(output[base+1]), int(output[base+2]), int(output[base+3])
		a1 := o0 + o2
		b1 := o0 - o2
		c1 := ((o1 * sinPI8Sqrt2) >> 16) - (o3 + ((o3 * cosPI8Sqrt2Minus1) >> 16))
		d1 := (o1 + ((o1 * cosPI8Sqrt2Minus1) >> 16)) + ((o3 * sinPI8Sqrt2) >> 16)

		dstRow := dst[y*dstStride : y*dstStride+4 : y*dstStride+4]
		predRow := pred[y*predStride : y*predStride+4 : y*predStride+4]
		dstRow[0] = byte(min(max(int(int16((a1+d1+4)>>3))+int(predRow[0]), 0), 255))
		dstRow[1] = byte(min(max(int(int16((b1+c1+4)>>3))+int(predRow[1]), 0), 255))
		dstRow[2] = byte(min(max(int(int16((b1-c1+4)>>3))+int(predRow[2]), 0), 255))
		dstRow[3] = byte(min(max(int(int16((a1-d1+4)>>3))+int(predRow[3]), 0), 255))
	}
}

// dcOnlyIDCT4x4AddScalar is the canonical scalar fast path used when only
// the DC coefficient is non-zero.
func dcOnlyIDCT4x4AddScalar(inputDC int16, pred []byte, predStride int, dst []byte, dstStride int) {
	a1 := int((inputDC + 4) >> 3)
	if predStride >= 0 && dstStride >= 0 && len(pred) >= 4 && predStride <= (len(pred)-4)/3 && len(dst) >= 4 && dstStride <= (len(dst)-4)/3 {
		if a1 == 0 && &pred[0] == &dst[0] && predStride == dstStride {
			return
		}
		p0, d0 := pred[:4], dst[:4]
		d0[0] = ClipPixel(a1 + int(p0[0]))
		d0[1] = ClipPixel(a1 + int(p0[1]))
		d0[2] = ClipPixel(a1 + int(p0[2]))
		d0[3] = ClipPixel(a1 + int(p0[3]))
		p1, d1 := pred[predStride:predStride+4], dst[dstStride:dstStride+4]
		d1[0] = ClipPixel(a1 + int(p1[0]))
		d1[1] = ClipPixel(a1 + int(p1[1]))
		d1[2] = ClipPixel(a1 + int(p1[2]))
		d1[3] = ClipPixel(a1 + int(p1[3]))
		p2, d2 := pred[2*predStride:2*predStride+4], dst[2*dstStride:2*dstStride+4]
		d2[0] = ClipPixel(a1 + int(p2[0]))
		d2[1] = ClipPixel(a1 + int(p2[1]))
		d2[2] = ClipPixel(a1 + int(p2[2]))
		d2[3] = ClipPixel(a1 + int(p2[3]))
		p3, d3 := pred[3*predStride:3*predStride+4], dst[3*dstStride:3*dstStride+4]
		d3[0] = ClipPixel(a1 + int(p3[0]))
		d3[1] = ClipPixel(a1 + int(p3[1]))
		d3[2] = ClipPixel(a1 + int(p3[2]))
		d3[3] = ClipPixel(a1 + int(p3[3]))
		return
	}
	for y := range 4 {
		dstRow := dst[y*dstStride : y*dstStride+4 : y*dstStride+4]
		predRow := pred[y*predStride : y*predStride+4 : y*predStride+4]
		dstRow[0] = ClipPixel(a1 + int(predRow[0]))
		dstRow[1] = ClipPixel(a1 + int(predRow[1]))
		dstRow[2] = ClipPixel(a1 + int(predRow[2]))
		dstRow[3] = ClipPixel(a1 + int(predRow[3]))
	}
}

func zeroDCSameWindow(pred []byte, ps int, dst []byte, ds int) bool {
	return ps == ds && ps >= 4 && len(pred) >= 4 && ps <= (len(pred)-4)/3 && len(dst) >= 4 && ds <= (len(dst)-4)/3 && &pred[0] == &dst[0]
}

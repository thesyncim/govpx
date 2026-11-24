package dsp

import "github.com/thesyncim/govpx/internal/pixel"

// Ported from libvpx v1.16.0 vp8/common/dequantize.c.

func DequantizeBlock(qcoeff *[16]int16, dequant *[16]int16, dqcoeff *[16]int16) {
	if pixel.Enabled && !dequantBlocksPartiallyOverlap(qcoeff, dqcoeff) && !dequantBlocksPartiallyOverlap(dequant, dqcoeff) {
		pixel.Mul16(qcoeff[:], dequant[:], dqcoeff[:], 16)
		return
	}

	for i := range 16 {
		dqcoeff[i] = qcoeff[i] * dequant[i]
	}
}

func dequantBlocksPartiallyOverlap(src, dst *[16]int16) bool {
	if src == dst {
		return false
	}
	for i := 1; i < 16; i++ {
		if &src[i] == &dst[0] || &dst[i] == &src[0] {
			return true
		}
	}
	return false
}

func DequantIDCT4x4Add(input *[16]int16, dequant *[16]int16, dst []byte, stride int) {
	// Sparse packets require only the DC multiply and add, not 15 zero products,
	// a full inverse transform, or clearing coefficients already known to be zero.
	if stride >= 4 && len(dst) >= 4 && stride <= (len(dst)-4)/3 {
		ac := input[1] | input[2] | input[3] | input[4] | input[5] | input[6] | input[7] | input[8] | input[9] | input[10] | input[11] | input[12] | input[13] | input[14] | input[15]
		if ac == 0 {
			dc := input[0] * dequant[0]
			DCOnlyIDCT4x4AddInt32(int32(dc), dst, stride, dst, stride)
			input[0] = 0
			return
		}
	}

	for i := range 16 {
		input[i] = dequant[i] * input[i]
	}

	IDCT4x4Add(input, dst, stride, dst, stride)

	for i := range 16 {
		input[i] = 0
	}
}

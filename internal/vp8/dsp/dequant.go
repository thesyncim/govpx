package dsp

// Ported from libvpx v1.16.0 vp8/common/dequantize.c.

func DequantizeBlock(qcoeff *[16]int16, dequant *[16]int16, dqcoeff *[16]int16) {
	for i := 0; i < 16; i++ {
		dqcoeff[i] = qcoeff[i] * dequant[i]
	}
}

func DequantIDCT4x4Add(input *[16]int16, dequant *[16]int16, dst []byte, stride int) {
	for i := 0; i < 16; i++ {
		input[i] = dequant[i] * input[i]
	}

	IDCT4x4Add(input, dst, stride, dst, stride)

	for i := 0; i < 16; i++ {
		input[i] = 0
	}
}

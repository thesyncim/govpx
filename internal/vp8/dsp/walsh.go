package dsp

// Ported from libvpx v1.16.0 vp8/common/idctllm.c.

// InverseWalsh4x4 dispatches to the SIMD or scalar 4x4 inverse Walsh-Hadamard
// kernel. The SIMD implementations mirror libvpx v1.16.0
// vp8/common/arm/neon/iwalsh_neon.c (vp8_short_inv_walsh4x4_neon) and
// vp8/common/x86/iwalsh_sse2.asm (vp8_short_inv_walsh4x4_sse2). Output is
// byte-identical to inverseWalsh4x4Scalar for the decoder's coefficient range.
func InverseWalsh4x4(input *[16]int16, mbDQCoeff []int16) {
	inverseWalsh4x4SIMD(input, mbDQCoeff)
}

// InverseWalsh4x4ToLumaDC computes the 4x4 inverse Walsh-Hadamard transform and
// writes the 16 DC values directly into dst[0..15][0] without an intermediate
// 256-int16 flat buffer.
func InverseWalsh4x4ToLumaDC(input *[16]int16, dst *[25][16]int16) {
	if inverseWalsh4ToLumaDCGo(input, dst) {
		return
	}
	var output [16]int16
	for i := range 4 {
		a1 := int(input[i+0]) + int(input[i+12])
		b1 := int(input[i+4]) + int(input[i+8])
		c1 := int(input[i+4]) - int(input[i+8])
		d1 := int(input[i+0]) - int(input[i+12])
		output[i+0] = int16(a1 + b1)
		output[i+4] = int16(c1 + d1)
		output[i+8] = int16(a1 - b1)
		output[i+12] = int16(d1 - c1)
	}
	for i := range 4 {
		base := i * 4
		a1 := int(output[base+0]) + int(output[base+3])
		b1 := int(output[base+1]) + int(output[base+2])
		c1 := int(output[base+1]) - int(output[base+2])
		d1 := int(output[base+0]) - int(output[base+3])
		dst[base+0][0] = int16((a1 + b1 + 3) >> 3)
		dst[base+1][0] = int16((c1 + d1 + 3) >> 3)
		dst[base+2][0] = int16((a1 - b1 + 3) >> 3)
		dst[base+3][0] = int16((d1 - c1 + 3) >> 3)
	}
}

// InverseWalsh4x4To16 computes the 4x4 inverse Walsh-Hadamard transform and
// writes the 16 DC values into a compact [16]int16 array.
func InverseWalsh4x4To16(input *[16]int16, dst *[16]int16) {
	var output [16]int16
	for i := range 4 {
		a1 := int(input[i+0]) + int(input[i+12])
		b1 := int(input[i+4]) + int(input[i+8])
		c1 := int(input[i+4]) - int(input[i+8])
		d1 := int(input[i+0]) - int(input[i+12])
		output[i+0] = int16(a1 + b1)
		output[i+4] = int16(c1 + d1)
		output[i+8] = int16(a1 - b1)
		output[i+12] = int16(d1 - c1)
	}
	for i := range 4 {
		base := i * 4
		a1 := int(output[base+0]) + int(output[base+3])
		b1 := int(output[base+1]) + int(output[base+2])
		c1 := int(output[base+1]) - int(output[base+2])
		d1 := int(output[base+0]) - int(output[base+3])
		dst[base+0] = int16((a1 + b1 + 3) >> 3)
		dst[base+1] = int16((c1 + d1 + 3) >> 3)
		dst[base+2] = int16((a1 - b1 + 3) >> 3)
		dst[base+3] = int16((d1 - c1 + 3) >> 3)
	}
}

// inverseWalsh4x4Scalar is the canonical scalar port of libvpx
// vp8/common/idctllm.c vp8_short_inv_walsh4x4_c. SIMD ports must produce
// byte-identical output for the decoder's coefficient range.
func inverseWalsh4x4Scalar(input *[16]int16, mbDQCoeff []int16) {
	_ = mbDQCoeff[15*16]

	var output [16]int16

	for i := range 4 {
		a1 := int(input[i+0]) + int(input[i+12])
		b1 := int(input[i+4]) + int(input[i+8])
		c1 := int(input[i+4]) - int(input[i+8])
		d1 := int(input[i+0]) - int(input[i+12])

		output[i+0] = int16(a1 + b1)
		output[i+4] = int16(c1 + d1)
		output[i+8] = int16(a1 - b1)
		output[i+12] = int16(d1 - c1)
	}

	for i := range 4 {
		base := i * 4
		a1 := int(output[base+0]) + int(output[base+3])
		b1 := int(output[base+1]) + int(output[base+2])
		c1 := int(output[base+1]) - int(output[base+2])
		d1 := int(output[base+0]) - int(output[base+3])

		a2 := a1 + b1
		b2 := c1 + d1
		c2 := a1 - b1
		d2 := d1 - c1

		output[base+0] = int16((a2 + 3) >> 3)
		output[base+1] = int16((b2 + 3) >> 3)
		output[base+2] = int16((c2 + 3) >> 3)
		output[base+3] = int16((d2 + 3) >> 3)
	}

	for i := range 16 {
		mbDQCoeff[i*16] = output[i]
	}
}

func DCOnlyInverseWalsh4x4(inputDC int16, mbDQCoeff []int16) {
	_ = mbDQCoeff[15*16]

	a1 := int16((int(inputDC) + 3) >> 3)
	for i := range 16 {
		mbDQCoeff[i*16] = a1
	}
}

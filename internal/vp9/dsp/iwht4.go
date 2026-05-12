package dsp

// Iwht4x4_16Add is the inverse 4x4 Walsh-Hadamard used by lossless VP9.
// Mirrors vpx_iwht4x4_16_add_c line-for-line: each row is unpacked,
// shifted right by UNIT_QUANT_SHIFT (2 bits), passed through the
// orthonormal 4-point IWHT (3.5 adds + 0.5 shifts per pixel per
// libvpx's comment), then a transpose column pass writes back into the
// pixel buffer with the +clip_pixel_add fold.
func Iwht4x4_16Add(input []int32, dest []uint8, stride int) {
	var output [16]int32

	ip := input
	op := output[:]
	for i := 0; i < 4; i++ {
		a1 := int64(int16(ip[0])) >> unitQuantShift
		c1 := int64(int16(ip[1])) >> unitQuantShift
		d1 := int64(int16(ip[2])) >> unitQuantShift
		b1 := int64(int16(ip[3])) >> unitQuantShift
		a1 += c1
		d1 -= b1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= b1
		d1 += c1
		op[0] = wrapLow(a1)
		op[1] = wrapLow(b1)
		op[2] = wrapLow(c1)
		op[3] = wrapLow(d1)
		ip = ip[4:]
		op = op[4:]
	}

	for i := 0; i < 4; i++ {
		a1 := int64(output[4*0+i])
		c1 := int64(output[4*1+i])
		d1 := int64(output[4*2+i])
		b1 := int64(output[4*3+i])
		a1 += c1
		d1 -= b1
		e1 := (a1 - d1) >> 1
		b1 = e1 - b1
		c1 = e1 - c1
		a1 -= b1
		d1 += c1
		dest[stride*0+i] = clipPixelAdd(dest[stride*0+i], wrapLow(a1))
		dest[stride*1+i] = clipPixelAdd(dest[stride*1+i], wrapLow(b1))
		dest[stride*2+i] = clipPixelAdd(dest[stride*2+i], wrapLow(c1))
		dest[stride*3+i] = clipPixelAdd(dest[stride*3+i], wrapLow(d1))
	}
}

// Iwht4x4_1Add is the DC-only fast path for the lossless 4x4 inverse
// Walsh-Hadamard. Matches vpx_iwht4x4_1_add_c.
func Iwht4x4_1Add(input []int32, dest []uint8, stride int) {
	var tmp [4]int32

	a1 := int64(int16(input[0])) >> unitQuantShift
	e1 := a1 >> 1
	a1 -= e1
	tmp[0] = wrapLow(a1)
	tmp[1] = wrapLow(e1)
	tmp[2] = wrapLow(e1)
	tmp[3] = wrapLow(e1)

	for i := 0; i < 4; i++ {
		e1 := int64(tmp[i]) >> 1
		a1 := int64(tmp[i]) - e1
		dest[stride*0+i] = clipPixelAdd(dest[stride*0+i], wrapLow(a1))
		dest[stride*1+i] = clipPixelAdd(dest[stride*1+i], wrapLow(e1))
		dest[stride*2+i] = clipPixelAdd(dest[stride*2+i], wrapLow(e1))
		dest[stride*3+i] = clipPixelAdd(dest[stride*3+i], wrapLow(e1))
	}
}

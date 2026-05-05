package encoder

// Ported from libvpx v1.16.0 vp8/encoder/dct.c.

func ForwardDCT4x4(input []int16, stride int, output *[16]int16) {
	var tmp [16]int

	for row := 0; row < 4; row++ {
		ip := row * stride
		op := row * 4
		a1 := (int(input[ip+0]) + int(input[ip+3])) * 8
		b1 := (int(input[ip+1]) + int(input[ip+2])) * 8
		c1 := (int(input[ip+1]) - int(input[ip+2])) * 8
		d1 := (int(input[ip+0]) - int(input[ip+3])) * 8

		tmp[op+0] = a1 + b1
		tmp[op+2] = a1 - b1
		tmp[op+1] = (c1*2217 + d1*5352 + 14500) >> 12
		tmp[op+3] = (d1*2217 - c1*5352 + 7500) >> 12
	}

	for col := 0; col < 4; col++ {
		a1 := tmp[col+0] + tmp[col+12]
		b1 := tmp[col+4] + tmp[col+8]
		c1 := tmp[col+4] - tmp[col+8]
		d1 := tmp[col+0] - tmp[col+12]

		output[col+0] = int16((a1 + b1 + 7) >> 4)
		output[col+8] = int16((a1 - b1 + 7) >> 4)
		output[col+4] = int16(((c1*2217 + d1*5352 + 12000) >> 16) + boolInt(d1 != 0))
		output[col+12] = int16((d1*2217 - c1*5352 + 51000) >> 16)
	}
}

func ForwardDCT8x4(input []int16, stride int, output *[32]int16) {
	var left [16]int16
	var right [16]int16
	ForwardDCT4x4(input, stride, &left)
	ForwardDCT4x4(input[4:], stride, &right)
	copy(output[0:16], left[:])
	copy(output[16:32], right[:])
}

func ForwardWalsh4x4(input []int16, stride int, output *[16]int16) {
	var tmp [16]int

	for row := 0; row < 4; row++ {
		ip := row * stride
		op := row * 4
		a1 := (int(input[ip+0]) + int(input[ip+2])) * 4
		d1 := (int(input[ip+1]) + int(input[ip+3])) * 4
		c1 := (int(input[ip+1]) - int(input[ip+3])) * 4
		b1 := (int(input[ip+0]) - int(input[ip+2])) * 4

		tmp[op+0] = a1 + d1 + boolInt(a1 != 0)
		tmp[op+1] = b1 + c1
		tmp[op+2] = b1 - c1
		tmp[op+3] = a1 - d1
	}

	for col := 0; col < 4; col++ {
		a1 := tmp[col+0] + tmp[col+8]
		d1 := tmp[col+4] + tmp[col+12]
		c1 := tmp[col+4] - tmp[col+12]
		b1 := tmp[col+0] - tmp[col+8]

		a2 := a1 + d1
		b2 := b1 + c1
		c2 := b1 - c1
		d2 := a1 - d1

		a2 += boolInt(a2 < 0)
		b2 += boolInt(b2 < 0)
		c2 += boolInt(c2 < 0)
		d2 += boolInt(d2 < 0)

		output[col+0] = int16((a2 + 3) >> 3)
		output[col+4] = int16((b2 + 3) >> 3)
		output[col+8] = int16((c2 + 3) >> 3)
		output[col+12] = int16((d2 + 3) >> 3)
	}
}

func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

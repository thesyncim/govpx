package encoder

// Pure-Go fallback dispatcher for the batched 4x4 forward DCT entry
// point. Mirrors libvpx v1.16.0 vp8/encoder/dct.c vp8_short_fdct4x4_c
// applied per block.

func forwardDCT4x4BatchSIMD(input []int16, output []int16, count int) {
	i := forwardDCT4BatchGo(input, output, count)
	if i >= count {
		return
	}
	_ = input[count*16-1]
	_ = output[count*16-1]
	for ; i < count; i++ {
		base := i * 16
		in := (*[16]int16)(input[base : base+16])
		out := (*[16]int16)(output[base : base+16])
		r00, r01, r02, r03 := int(in[0]), int(in[1]), int(in[2]), int(in[3])
		r10, r11, r12, r13 := int(in[4]), int(in[5]), int(in[6]), int(in[7])
		r20, r21, r22, r23 := int(in[8]), int(in[9]), int(in[10]), int(in[11])
		r30, r31, r32, r33 := int(in[12]), int(in[13]), int(in[14]), int(in[15])
		a0, b0, c0, d0 := r00+r03, r01+r02, r01-r02, r00-r03
		t0, t2 := (a0+b0)<<3, (a0-b0)<<3
		t1 := (c0*17736 + d0*42816 + 14500) >> 12
		t3 := (d0*17736 - c0*42816 + 7500) >> 12

		a1, b1, c1, d1 := r10+r13, r11+r12, r11-r12, r10-r13
		t4, t6 := (a1+b1)<<3, (a1-b1)<<3
		t5 := (c1*17736 + d1*42816 + 14500) >> 12
		t7 := (d1*17736 - c1*42816 + 7500) >> 12

		a2, b2, c2, d2 := r20+r23, r21+r22, r21-r22, r20-r23
		t8, t10 := (a2+b2)<<3, (a2-b2)<<3
		t9 := (c2*17736 + d2*42816 + 14500) >> 12
		t11 := (d2*17736 - c2*42816 + 7500) >> 12

		a3, b3, c3, d3 := r30+r33, r31+r32, r31-r32, r30-r33
		t12, t14 := (a3+b3)<<3, (a3-b3)<<3
		t13 := (c3*17736 + d3*42816 + 14500) >> 12
		t15 := (d3*17736 - c3*42816 + 7500) >> 12

		// col 0
		ca, cb, cc, cd := t0+t12, t4+t8, t4-t8, t0-t12
		out[0] = int16((ca + cb + 7) >> 4)
		out[8] = int16((ca - cb + 7) >> 4)
		v := (cc*2217 + cd*5352 + 12000) >> 16
		if cd != 0 {
			v++
		}
		out[4] = int16(v)
		out[12] = int16((cd*2217 - cc*5352 + 51000) >> 16)

		// col 1
		ca, cb, cc, cd = t1+t13, t5+t9, t5-t9, t1-t13
		out[1] = int16((ca + cb + 7) >> 4)
		out[9] = int16((ca - cb + 7) >> 4)
		v = (cc*2217 + cd*5352 + 12000) >> 16
		if cd != 0 {
			v++
		}
		out[5] = int16(v)
		out[13] = int16((cd*2217 - cc*5352 + 51000) >> 16)

		// col 2
		ca, cb, cc, cd = t2+t14, t6+t10, t6-t10, t2-t14
		out[2] = int16((ca + cb + 7) >> 4)
		out[10] = int16((ca - cb + 7) >> 4)
		v = (cc*2217 + cd*5352 + 12000) >> 16
		if cd != 0 {
			v++
		}
		out[6] = int16(v)
		out[14] = int16((cd*2217 - cc*5352 + 51000) >> 16)

		// col 3
		ca, cb, cc, cd = t3+t15, t7+t11, t7-t11, t3-t15
		out[3] = int16((ca + cb + 7) >> 4)
		out[11] = int16((ca - cb + 7) >> 4)
		v = (cc*2217 + cd*5352 + 12000) >> 16
		if cd != 0 {
			v++
		}
		out[7] = int16(v)
		out[15] = int16((cd*2217 - cc*5352 + 51000) >> 16)
	}
}

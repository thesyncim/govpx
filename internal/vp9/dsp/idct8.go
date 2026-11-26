package dsp

import "encoding/binary"

// idct8 implements libvpx's idct8_c — the 8-point 1-D inverse DCT used
// by every Tx8x8 reconstruction. Four-stage butterfly with cospi
// multiplies at stages 1, 2 and 3; stages collapse into the final
// output via additions only. Matches vpx_dsp/inv_txfm.c stage-by-stage.
//
// Per libvpx, step1 / step2 are int16_t storage (truncated on store),
// so step values are clamped to the int16 range mid-pipeline — we
// mirror that with explicit int16 narrowing to preserve byte parity.
func idct8(input, output []int16) {
	_ = input[7]
	_ = output[7]
	in0 := int64(input[0])
	in1 := int64(input[1])
	in2 := int64(input[2])
	in3 := int64(input[3])
	in4 := int64(input[4])
	in5 := int64(input[5])
	in6 := int64(input[6])
	in7 := int64(input[7])

	// stage 1
	s1_0 := int64(int16(in0))
	s1_2 := int64(int16(in4))
	s1_1 := int64(int16(in2))
	s1_3 := int64(int16(in6))
	s1_4 := int64(int16(dctConstRoundShift(in1*cospi28_64 - in7*cospi4_64)))
	s1_7 := int64(int16(dctConstRoundShift(in1*cospi4_64 + in7*cospi28_64)))
	s1_5 := int64(int16(dctConstRoundShift(in5*cospi12_64 - in3*cospi20_64)))
	s1_6 := int64(int16(dctConstRoundShift(in5*cospi20_64 + in3*cospi12_64)))

	// stage 2
	s2_0 := int64(int16(dctConstRoundShift((s1_0 + s1_2) * cospi16_64)))
	s2_1 := int64(int16(dctConstRoundShift((s1_0 - s1_2) * cospi16_64)))
	s2_2 := int64(int16(dctConstRoundShift(s1_1*cospi24_64 - s1_3*cospi8_64)))
	s2_3 := int64(int16(dctConstRoundShift(s1_1*cospi8_64 + s1_3*cospi24_64)))
	s2_4 := int64(int16(s1_4 + s1_5))
	s2_5 := int64(int16(s1_4 - s1_5))
	s2_6 := int64(int16(s1_7 - s1_6))
	s2_7 := int64(int16(s1_6 + s1_7))

	// stage 3
	t0 := int64(int16(s2_0 + s2_3))
	t1 := int64(int16(s2_1 + s2_2))
	t2 := int64(int16(s2_1 - s2_2))
	t3 := int64(int16(s2_0 - s2_3))
	t4 := s2_4
	t5 := int64(int16(dctConstRoundShift((s2_6 - s2_5) * cospi16_64)))
	t6 := int64(int16(dctConstRoundShift((s2_5 + s2_6) * cospi16_64)))
	t7 := s2_7

	// stage 4
	output[0] = int16(t0 + t7)
	output[1] = int16(t1 + t6)
	output[2] = int16(t2 + t5)
	output[3] = int16(t3 + t4)
	output[4] = int16(t3 - t4)
	output[5] = int16(t2 - t5)
	output[6] = int16(t1 - t6)
	output[7] = int16(t0 - t7)
}

// idct8x8_64AddScalar applies the full 8x8 inverse DCT to a
// 64-coefficient block and adds the result onto dest. Mirrors
// vpx_idct8x8_64_add_c: row pass into a transposed intermediate, then
// a column pass with ROUND_POWER_OF_TWO(., 5) into clip_pixel_add.
//
// Scalar reference; the exported Idct8x8_64Add wrapper is in
// idct_dispatch_*.go.
func idct8x8_64AddScalar(input []int16, dest []uint8, stride int) {
	_ = input[63]
	_ = dest[7*stride+7]
	var out [64]int16
	for i := range 8 {
		row := input[i*8 : i*8+8]
		in0, in1, in2, in3 := int64(row[0]), int64(row[1]), int64(row[2]), int64(row[3])
		in4, in5, in6, in7 := int64(row[4]), int64(row[5]), int64(row[6]), int64(row[7])
		if in1|in2|in3|in4|in5|in6|in7 == 0 {
			if in0 != 0 {
				dc := int16(dctConstRoundShift(in0 * cospi16_64))
				out[0*8+i] = dc
				out[1*8+i] = dc
				out[2*8+i] = dc
				out[3*8+i] = dc
				out[4*8+i] = dc
				out[5*8+i] = dc
				out[6*8+i] = dc
				out[7*8+i] = dc
			}
			continue
		}

		s1_4 := int64(int16(dctConstRoundShift(in1*cospi28_64 - in7*cospi4_64)))
		s1_7 := int64(int16(dctConstRoundShift(in1*cospi4_64 + in7*cospi28_64)))
		s1_5 := int64(int16(dctConstRoundShift(in5*cospi12_64 - in3*cospi20_64)))
		s1_6 := int64(int16(dctConstRoundShift(in5*cospi20_64 + in3*cospi12_64)))

		s2_0 := int64(int16(dctConstRoundShift((in0 + in4) * cospi16_64)))
		s2_1 := int64(int16(dctConstRoundShift((in0 - in4) * cospi16_64)))
		s2_2 := int64(int16(dctConstRoundShift(in2*cospi24_64 - in6*cospi8_64)))
		s2_3 := int64(int16(dctConstRoundShift(in2*cospi8_64 + in6*cospi24_64)))
		s2_4 := int64(int16(s1_4 + s1_5))
		s2_5 := int64(int16(s1_4 - s1_5))
		s2_6 := int64(int16(s1_7 - s1_6))
		s2_7 := int64(int16(s1_6 + s1_7))

		t0 := int64(int16(s2_0 + s2_3))
		t1 := int64(int16(s2_1 + s2_2))
		t2 := int64(int16(s2_1 - s2_2))
		t3 := int64(int16(s2_0 - s2_3))
		t5 := int64(int16(dctConstRoundShift((s2_6 - s2_5) * cospi16_64)))
		t6 := int64(int16(dctConstRoundShift((s2_5 + s2_6) * cospi16_64)))

		// Write transposed so column pass reads contiguous rows.
		out[0*8+i] = int16(t0 + s2_7)
		out[1*8+i] = int16(t1 + t6)
		out[2*8+i] = int16(t2 + t5)
		out[3*8+i] = int16(t3 + s2_4)
		out[4*8+i] = int16(t3 - s2_4)
		out[5*8+i] = int16(t2 - t5)
		out[6*8+i] = int16(t1 - t6)
		out[7*8+i] = int16(t0 - s2_7)
	}
	s1, s2, s3, s4, s5, s6, s7 := stride, 2*stride, 3*stride, 4*stride, 5*stride, 6*stride, 7*stride
	for i := range 8 {
		col := (*[8]int16)(out[i*8 : i*8+8])
		in0, in1, in2, in3 := int64(col[0]), int64(col[1]), int64(col[2]), int64(col[3])
		in4, in5, in6, in7 := int64(col[4]), int64(col[5]), int64(col[6]), int64(col[7])
		if in1|in2|in3|in4|in5|in6|in7 == 0 {
			if in0 != 0 {
				dc := roundPowerOfTwo(int32(int16(dctConstRoundShift(in0*cospi16_64))), 5)
				dest[i] = clipPixelAdd(dest[i], dc)
				dest[s1+i] = clipPixelAdd(dest[s1+i], dc)
				dest[s2+i] = clipPixelAdd(dest[s2+i], dc)
				dest[s3+i] = clipPixelAdd(dest[s3+i], dc)
				dest[s4+i] = clipPixelAdd(dest[s4+i], dc)
				dest[s5+i] = clipPixelAdd(dest[s5+i], dc)
				dest[s6+i] = clipPixelAdd(dest[s6+i], dc)
				dest[s7+i] = clipPixelAdd(dest[s7+i], dc)
			}
			continue
		}

		s1_4 := int64(int16(dctConstRoundShift(in1*cospi28_64 - in7*cospi4_64)))
		s1_7 := int64(int16(dctConstRoundShift(in1*cospi4_64 + in7*cospi28_64)))
		s1_5 := int64(int16(dctConstRoundShift(in5*cospi12_64 - in3*cospi20_64)))
		s1_6 := int64(int16(dctConstRoundShift(in5*cospi20_64 + in3*cospi12_64)))

		s2_0 := int64(int16(dctConstRoundShift((in0 + in4) * cospi16_64)))
		s2_1 := int64(int16(dctConstRoundShift((in0 - in4) * cospi16_64)))
		s2_2 := int64(int16(dctConstRoundShift(in2*cospi24_64 - in6*cospi8_64)))
		s2_3 := int64(int16(dctConstRoundShift(in2*cospi8_64 + in6*cospi24_64)))
		s2_4 := int64(int16(s1_4 + s1_5))
		s2_5 := int64(int16(s1_4 - s1_5))
		s2_6 := int64(int16(s1_7 - s1_6))
		s2_7 := int64(int16(s1_6 + s1_7))

		t0 := int64(int16(s2_0 + s2_3))
		t1 := int64(int16(s2_1 + s2_2))
		t2 := int64(int16(s2_1 - s2_2))
		t3 := int64(int16(s2_0 - s2_3))
		t5 := int64(int16(dctConstRoundShift((s2_6 - s2_5) * cospi16_64)))
		t6 := int64(int16(dctConstRoundShift((s2_5 + s2_6) * cospi16_64)))

		dest[i] = clipPixelAdd(dest[i], roundPowerOfTwo(int32(int16(t0+s2_7)), 5))
		dest[s1+i] = clipPixelAdd(dest[s1+i], roundPowerOfTwo(int32(int16(t1+t6)), 5))
		dest[s2+i] = clipPixelAdd(dest[s2+i], roundPowerOfTwo(int32(int16(t2+t5)), 5))
		dest[s3+i] = clipPixelAdd(dest[s3+i], roundPowerOfTwo(int32(int16(t3+s2_4)), 5))
		dest[s4+i] = clipPixelAdd(dest[s4+i], roundPowerOfTwo(int32(int16(t3-s2_4)), 5))
		dest[s5+i] = clipPixelAdd(dest[s5+i], roundPowerOfTwo(int32(int16(t2-t5)), 5))
		dest[s6+i] = clipPixelAdd(dest[s6+i], roundPowerOfTwo(int32(int16(t1-t6)), 5))
		dest[s7+i] = clipPixelAdd(dest[s7+i], roundPowerOfTwo(int32(int16(t0-s2_7)), 5))
	}
}

// idct8x8_12AddScalar is the sparse fast path used when only the
// top-left 4x4 of the input is non-zero. Matches vpx_idct8x8_12_add_c:
// it skips the lower 4 rows of the row pass and zero-fills out before
// the column pass.
//
// Scalar reference; the exported Idct8x8_12Add wrapper is in
// idct_dispatch_*.go.
func idct8x8_12AddScalar(input []int16, dest []uint8, stride int) {
	_ = input[31]
	_ = dest[7*stride+7]
	var out [32]int16 // 8 columns × 4 non-zero rows (transposed)
	for i := range 4 {
		row := input[i*8 : i*8+8]
		in0, in1, in2, in3 := int64(row[0]), int64(row[1]), int64(row[2]), int64(row[3])
		in4, in5, in6, in7 := int64(row[4]), int64(row[5]), int64(row[6]), int64(row[7])
		if in4|in5|in6|in7 == 0 {
			if in1|in2|in3 == 0 {
				if in0 != 0 {
					dc := int16(dctConstRoundShift(in0 * cospi16_64))
					out[0*4+i] = dc
					out[1*4+i] = dc
					out[2*4+i] = dc
					out[3*4+i] = dc
					out[4*4+i] = dc
					out[5*4+i] = dc
					out[6*4+i] = dc
					out[7*4+i] = dc
				}
				continue
			}
			s1_4 := int64(int16(dctConstRoundShift(in1 * cospi28_64)))
			s1_7 := int64(int16(dctConstRoundShift(in1 * cospi4_64)))
			s1_5 := int64(int16(dctConstRoundShift(-in3 * cospi20_64)))
			s1_6 := int64(int16(dctConstRoundShift(in3 * cospi12_64)))

			s2_0 := int64(int16(dctConstRoundShift(in0 * cospi16_64)))
			s2_2 := int64(int16(dctConstRoundShift(in2 * cospi24_64)))
			s2_3 := int64(int16(dctConstRoundShift(in2 * cospi8_64)))
			s2_4 := int64(int16(s1_4 + s1_5))
			s2_5 := int64(int16(s1_4 - s1_5))
			s2_6 := int64(int16(s1_7 - s1_6))
			s2_7 := int64(int16(s1_6 + s1_7))

			t0 := int64(int16(s2_0 + s2_3))
			t1 := int64(int16(s2_0 + s2_2))
			t2 := int64(int16(s2_0 - s2_2))
			t3 := int64(int16(s2_0 - s2_3))
			t5 := int64(int16(dctConstRoundShift((s2_6 - s2_5) * cospi16_64)))
			t6 := int64(int16(dctConstRoundShift((s2_5 + s2_6) * cospi16_64)))

			out[0*4+i] = int16(t0 + s2_7)
			out[1*4+i] = int16(t1 + t6)
			out[2*4+i] = int16(t2 + t5)
			out[3*4+i] = int16(t3 + s2_4)
			out[4*4+i] = int16(t3 - s2_4)
			out[5*4+i] = int16(t2 - t5)
			out[6*4+i] = int16(t1 - t6)
			out[7*4+i] = int16(t0 - s2_7)
			continue
		}

		s1_4 := int64(int16(dctConstRoundShift(in1*cospi28_64 - in7*cospi4_64)))
		s1_7 := int64(int16(dctConstRoundShift(in1*cospi4_64 + in7*cospi28_64)))
		s1_5 := int64(int16(dctConstRoundShift(in5*cospi12_64 - in3*cospi20_64)))
		s1_6 := int64(int16(dctConstRoundShift(in5*cospi20_64 + in3*cospi12_64)))

		s2_0 := int64(int16(dctConstRoundShift((in0 + in4) * cospi16_64)))
		s2_1 := int64(int16(dctConstRoundShift((in0 - in4) * cospi16_64)))
		s2_2 := int64(int16(dctConstRoundShift(in2*cospi24_64 - in6*cospi8_64)))
		s2_3 := int64(int16(dctConstRoundShift(in2*cospi8_64 + in6*cospi24_64)))
		s2_4 := int64(int16(s1_4 + s1_5))
		s2_5 := int64(int16(s1_4 - s1_5))
		s2_6 := int64(int16(s1_7 - s1_6))
		s2_7 := int64(int16(s1_6 + s1_7))

		t0 := int64(int16(s2_0 + s2_3))
		t1 := int64(int16(s2_1 + s2_2))
		t2 := int64(int16(s2_1 - s2_2))
		t3 := int64(int16(s2_0 - s2_3))
		t5 := int64(int16(dctConstRoundShift((s2_6 - s2_5) * cospi16_64)))
		t6 := int64(int16(dctConstRoundShift((s2_5 + s2_6) * cospi16_64)))

		out[0*4+i] = int16(t0 + s2_7)
		out[1*4+i] = int16(t1 + t6)
		out[2*4+i] = int16(t2 + t5)
		out[3*4+i] = int16(t3 + s2_4)
		out[4*4+i] = int16(t3 - s2_4)
		out[5*4+i] = int16(t2 - t5)
		out[6*4+i] = int16(t1 - t6)
		out[7*4+i] = int16(t0 - s2_7)
	}
	s1, s2, s3, s4, s5, s6, s7 := stride, 2*stride, 3*stride, 4*stride, 5*stride, 6*stride, 7*stride
	for i := range 8 {
		col := out[i*4 : i*4+4]
		in0, in1, in2, in3 := int64(col[0]), int64(col[1]), int64(col[2]), int64(col[3])

		s1_4 := int64(int16(dctConstRoundShift(in1 * cospi28_64)))
		s1_7 := int64(int16(dctConstRoundShift(in1 * cospi4_64)))
		s1_5 := int64(int16(dctConstRoundShift(-in3 * cospi20_64)))
		s1_6 := int64(int16(dctConstRoundShift(in3 * cospi12_64)))

		s2_0 := int64(int16(dctConstRoundShift(in0 * cospi16_64)))
		s2_2 := int64(int16(dctConstRoundShift(in2 * cospi24_64)))
		s2_3 := int64(int16(dctConstRoundShift(in2 * cospi8_64)))
		s2_4 := int64(int16(s1_4 + s1_5))
		s2_5 := int64(int16(s1_4 - s1_5))
		s2_6 := int64(int16(s1_7 - s1_6))
		s2_7 := int64(int16(s1_6 + s1_7))

		t0 := int64(int16(s2_0 + s2_3))
		t1 := int64(int16(s2_0 + s2_2))
		t2 := int64(int16(s2_0 - s2_2))
		t3 := int64(int16(s2_0 - s2_3))
		t5 := int64(int16(dctConstRoundShift((s2_6 - s2_5) * cospi16_64)))
		t6 := int64(int16(dctConstRoundShift((s2_5 + s2_6) * cospi16_64)))

		dest[i] = clipPixelAdd(dest[i], roundPowerOfTwo(int32(int16(t0+s2_7)), 5))
		dest[s1+i] = clipPixelAdd(dest[s1+i], roundPowerOfTwo(int32(int16(t1+t6)), 5))
		dest[s2+i] = clipPixelAdd(dest[s2+i], roundPowerOfTwo(int32(int16(t2+t5)), 5))
		dest[s3+i] = clipPixelAdd(dest[s3+i], roundPowerOfTwo(int32(int16(t3+s2_4)), 5))
		dest[s4+i] = clipPixelAdd(dest[s4+i], roundPowerOfTwo(int32(int16(t3-s2_4)), 5))
		dest[s5+i] = clipPixelAdd(dest[s5+i], roundPowerOfTwo(int32(int16(t2-t5)), 5))
		dest[s6+i] = clipPixelAdd(dest[s6+i], roundPowerOfTwo(int32(int16(t1-t6)), 5))
		dest[s7+i] = clipPixelAdd(dest[s7+i], roundPowerOfTwo(int32(int16(t0-s2_7)), 5))
	}
}

// idct8x8_1AddScalar is the DC-only fast path. Matches
// vpx_idct8x8_1_add_c.
//
// Scalar reference; the exported Idct8x8_1Add wrapper is in
// idct_dispatch_*.go.
func idct8x8_1AddScalar(input []int16, dest []uint8, stride int) {
	out := int16(dctConstRoundShift(int64(input[0]) * cospi16_64))
	out = int16(dctConstRoundShift(int64(out) * cospi16_64))
	a1 := roundPowerOfTwo(int32(out), 5)
	if a1 == 0 {
		return
	}
	const (
		mask00FF = uint64(0x00ff00ff00ff00ff)
		maskFF00 = uint64(0xff00ff00ff00ff00)
		bit0100  = uint64(0x0100010001000100)
		ones16   = uint64(0x0001000100010001)
	)
	if a1 > 0 && a1 <= 255 {
		d := uint64(a1) * ones16
		di := 0
		for range 8 {
			row := dest[di : di+8]
			w := binary.LittleEndian.Uint64(row)
			lo := (w & mask00FF) + d
			lo = (lo | (((lo & bit0100) >> 8) * 0xff)) & mask00FF
			hi := ((w >> 8) & mask00FF) + d
			hi = ((hi | (((hi & bit0100) >> 8) * 0xff)) << 8) & maskFF00
			binary.LittleEndian.PutUint64(row, lo|hi)
			di += stride
		}
		return
	}
	if a1 < 0 && a1 >= -255 {
		d := uint64(-a1) * ones16
		di := 0
		for range 8 {
			row := dest[di : di+8]
			w := binary.LittleEndian.Uint64(row)
			lo := ((w & mask00FF) | bit0100) - d
			lo = lo & (((lo & bit0100) >> 8) * 0xff)
			hi := (((w >> 8) & mask00FF) | bit0100) - d
			hi = (hi & (((hi & bit0100) >> 8) * 0xff)) << 8
			binary.LittleEndian.PutUint64(row, lo|hi)
			di += stride
		}
		return
	}
	di := 0
	for range 8 {
		row := dest[di : di+8]
		_ = row[7]
		row[0] = clipPixelAdd(row[0], a1)
		row[1] = clipPixelAdd(row[1], a1)
		row[2] = clipPixelAdd(row[2], a1)
		row[3] = clipPixelAdd(row[3], a1)
		row[4] = clipPixelAdd(row[4], a1)
		row[5] = clipPixelAdd(row[5], a1)
		row[6] = clipPixelAdd(row[6], a1)
		row[7] = clipPixelAdd(row[7], a1)
		di += stride
	}
}

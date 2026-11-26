package pixel

// Squared-error-only references retain the codec's modulo-2^32 result.
func SSE16Scalar(src []byte, ss int, ref []byte, rs, h int) (sse uint32) {
	var s0, s1, s2, s3 int
	si, ri := 0, 0
	for range h {
		s := src[si : si+16]
		r := ref[ri : ri+16]
		_ = s[15]
		_ = r[15]
		d0 := int(s[0]) - int(r[0])
		d1 := int(s[1]) - int(r[1])
		d2 := int(s[2]) - int(r[2])
		d3 := int(s[3]) - int(r[3])
		s0 += d0 * d0
		s1 += d1 * d1
		s2 += d2 * d2
		s3 += d3 * d3
		d4 := int(s[4]) - int(r[4])
		d5 := int(s[5]) - int(r[5])
		d6 := int(s[6]) - int(r[6])
		d7 := int(s[7]) - int(r[7])
		s0 += d4 * d4
		s1 += d5 * d5
		s2 += d6 * d6
		s3 += d7 * d7
		d8 := int(s[8]) - int(r[8])
		d9 := int(s[9]) - int(r[9])
		d10 := int(s[10]) - int(r[10])
		d11 := int(s[11]) - int(r[11])
		s0 += d8 * d8
		s1 += d9 * d9
		s2 += d10 * d10
		s3 += d11 * d11
		d12 := int(s[12]) - int(r[12])
		d13 := int(s[13]) - int(r[13])
		d14 := int(s[14]) - int(r[14])
		d15 := int(s[15]) - int(r[15])
		s0 += d12 * d12
		s1 += d13 * d13
		s2 += d14 * d14
		s3 += d15 * d15
		si += ss
		ri += rs
	}
	return uint32(s0 + s1 + s2 + s3)
}

func SSE8Scalar(src []byte, ss int, ref []byte, rs, h int) (sse uint32) {
	var s0, s1, s2, s3 int
	si, ri := 0, 0
	for range h {
		s := src[si : si+8]
		r := ref[ri : ri+8]
		_ = s[7]
		_ = r[7]
		d0 := int(s[0]) - int(r[0])
		d1 := int(s[1]) - int(r[1])
		d2 := int(s[2]) - int(r[2])
		d3 := int(s[3]) - int(r[3])
		s0 += d0 * d0
		s1 += d1 * d1
		s2 += d2 * d2
		s3 += d3 * d3
		d4 := int(s[4]) - int(r[4])
		d5 := int(s[5]) - int(r[5])
		d6 := int(s[6]) - int(r[6])
		d7 := int(s[7]) - int(r[7])
		s0 += d4 * d4
		s1 += d5 * d5
		s2 += d6 * d6
		s3 += d7 * d7
		si += ss
		ri += rs
	}
	return uint32(s0 + s1 + s2 + s3)
}

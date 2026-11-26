package pixel

import "encoding/binary"

// DiffMetrics computes SAD, signed residual sum and SSE in one block walk.
// Codec blocks up to 64x64 use vector accumulators; larger inputs retain the
// scalar contract. Input rows may have arbitrary strides and exact end lengths.
func DiffMetrics(src []byte, ss int, ref []byte, rs, w, h int) (sad, sum int, sse uint32) {
	if Enabled && w > 0 && h > 0 && w <= 64 && h <= 64 {
		return diffMetricsSIMD(src, ss, ref, rs, w, h)
	}
	if w <= 0 || h <= 0 {
		return 0, 0, 0
	}
	if w&15 == 0 {
		var sadTotal uint64
		var sum0, sum1, sse0, sse1, sse2, sse3 int
		si, ri := 0, 0
		for range h {
			sRow := src[si : si+w]
			rRow := ref[ri : ri+w]
			for x := 0; x < w; x += 16 {
				s := (*[16]byte)(sRow[x : x+16])
				r := (*[16]byte)(rRow[x : x+16])
				sw0 := binary.LittleEndian.Uint64(s[0:8])
				rw0 := binary.LittleEndian.Uint64(r[0:8])
				sw1 := binary.LittleEndian.Uint64(s[8:16])
				rw1 := binary.LittleEndian.Uint64(r[8:16])
				acc := sadSWAR4(sw0&0x00ff00ff00ff00ff, rw0&0x00ff00ff00ff00ff) +
					sadSWAR4((sw0>>8)&0x00ff00ff00ff00ff, (rw0>>8)&0x00ff00ff00ff00ff) +
					sadSWAR4(sw1&0x00ff00ff00ff00ff, rw1&0x00ff00ff00ff00ff) +
					sadSWAR4((sw1>>8)&0x00ff00ff00ff00ff, (rw1>>8)&0x00ff00ff00ff00ff)
				sadTotal += (acc * 0x0001000100010001) >> 48

				d0 := int(s[0]) - int(r[0])
				d1 := int(s[1]) - int(r[1])
				d2 := int(s[2]) - int(r[2])
				d3 := int(s[3]) - int(r[3])
				sum0 += d0 + d1 + d2 + d3
				sse0 += d0 * d0
				sse1 += d1 * d1
				sse2 += d2 * d2
				sse3 += d3 * d3
				d4 := int(s[4]) - int(r[4])
				d5 := int(s[5]) - int(r[5])
				d6 := int(s[6]) - int(r[6])
				d7 := int(s[7]) - int(r[7])
				sum1 += d4 + d5 + d6 + d7
				sse0 += d4 * d4
				sse1 += d5 * d5
				sse2 += d6 * d6
				sse3 += d7 * d7
				d8 := int(s[8]) - int(r[8])
				d9 := int(s[9]) - int(r[9])
				d10 := int(s[10]) - int(r[10])
				d11 := int(s[11]) - int(r[11])
				sum0 += d8 + d9 + d10 + d11
				sse0 += d8 * d8
				sse1 += d9 * d9
				sse2 += d10 * d10
				sse3 += d11 * d11
				d12 := int(s[12]) - int(r[12])
				d13 := int(s[13]) - int(r[13])
				d14 := int(s[14]) - int(r[14])
				d15 := int(s[15]) - int(r[15])
				sum1 += d12 + d13 + d14 + d15
				sse0 += d12 * d12
				sse1 += d13 * d13
				sse2 += d14 * d14
				sse3 += d15 * d15
			}
			si += ss
			ri += rs
		}
		return int(sadTotal), sum0 + sum1, uint32(sse0 + sse1 + sse2 + sse3)
	}
	var sad0, sad1, sum0, sse0, sse1 int
	si, ri := 0, 0
	for range h {
		s := src[si : si+w]
		r := ref[ri : ri+w]
		x := 0
		for ; x+3 < w; x += 4 {
			d0 := int(s[x]) - int(r[x])
			d1 := int(s[x+1]) - int(r[x+1])
			d2 := int(s[x+2]) - int(r[x+2])
			d3 := int(s[x+3]) - int(r[x+3])
			sad0 += max(d0, -d0) + max(d2, -d2)
			sad1 += max(d1, -d1) + max(d3, -d3)
			sum0 += d0 + d1 + d2 + d3
			sse0 += d0*d0 + d2*d2
			sse1 += d1*d1 + d3*d3
		}
		for ; x < w; x++ {
			d := int(s[x]) - int(r[x])
			sad0 += max(d, -d)
			sum0 += d
			sse0 += d * d
		}
		si += ss
		ri += rs
	}
	return sad0 + sad1, sum0, uint32(sse0 + sse1)
}

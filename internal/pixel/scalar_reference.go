// Independent scalar pixel kernels, also used for CPUs without AVX2.
package pixel

import "encoding/binary"

var filterClipLUT = func() [512]byte {
	var lut [512]byte
	for i := range 512 {
		v := i - 128
		if v < 0 {
			v = 0
		} else if v > 255 {
			v = 255
		}
		lut[i] = byte(v)
	}
	return lut
}()

func sadSWAR4(a, b uint64) uint64 {
	const (
		mask00FF = uint64(0x00ff00ff00ff00ff)
		bit0100  = uint64(0x0100010001000100)
	)
	d := (a | bit0100) - b
	lt := (^d) & bit0100
	lt8 := lt >> 8
	return ((d & mask00FF) ^ (lt - lt8)) + lt8
}

func sumLanes16(v uint64) int {
	return int((v & 0xffff) + ((v >> 16) & 0xffff) + ((v >> 32) & 0xffff) + (v >> 48))
}

func sad8Scalar(src []byte, ss int, ref []byte, rs, h int) int {
	const mask00FF = uint64(0x00ff00ff00ff00ff)
	var accLo, accHi uint64
	si, ri := 0, 0
	for range h {
		a := binary.LittleEndian.Uint64(src[si : si+8])
		b := binary.LittleEndian.Uint64(ref[ri : ri+8])
		accLo += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		si += ss
		ri += rs
	}
	return sumLanes16(accLo) + sumLanes16(accHi)
}

func sad32Scalar(src []byte, ss int, ref []byte, rs, h int) int {
	const mask00FF = uint64(0x00ff00ff00ff00ff)
	var accLo, accHi uint64
	si, ri := 0, 0
	for range h {
		s := src[si : si+32]
		r := ref[ri : ri+32]
		a := binary.LittleEndian.Uint64(s[0:8])
		b := binary.LittleEndian.Uint64(r[0:8])
		accLo += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[8:16])
		b = binary.LittleEndian.Uint64(r[8:16])
		accLo += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[16:24])
		b = binary.LittleEndian.Uint64(r[16:24])
		accLo += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[24:32])
		b = binary.LittleEndian.Uint64(r[24:32])
		accLo += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		si += ss
		ri += rs
	}
	return sumLanes16(accLo) + sumLanes16(accHi)
}

func sad64Scalar(src []byte, ss int, ref []byte, rs, h int) int {
	const mask00FF = uint64(0x00ff00ff00ff00ff)
	var accLo0, accHi0, accLo1, accHi1 uint64
	si, ri := 0, 0
	for range h {
		s := src[si : si+64]
		r := ref[ri : ri+64]
		a := binary.LittleEndian.Uint64(s[0:8])
		b := binary.LittleEndian.Uint64(r[0:8])
		accLo0 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi0 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[8:16])
		b = binary.LittleEndian.Uint64(r[8:16])
		accLo0 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi0 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[16:24])
		b = binary.LittleEndian.Uint64(r[16:24])
		accLo0 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi0 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[24:32])
		b = binary.LittleEndian.Uint64(r[24:32])
		accLo0 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi0 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[32:40])
		b = binary.LittleEndian.Uint64(r[32:40])
		accLo1 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi1 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[40:48])
		b = binary.LittleEndian.Uint64(r[40:48])
		accLo1 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi1 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[48:56])
		b = binary.LittleEndian.Uint64(r[48:56])
		accLo1 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi1 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		a = binary.LittleEndian.Uint64(s[56:64])
		b = binary.LittleEndian.Uint64(r[56:64])
		accLo1 += sadSWAR4(a&mask00FF, b&mask00FF)
		accHi1 += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
		si += ss
		ri += rs
	}
	return sumLanes16(accLo0) + sumLanes16(accHi0) + sumLanes16(accLo1) + sumLanes16(accHi1)
}

func SADScalar(src []byte, ss int, ref []byte, rs, w, h int) (sum int) {
	if w <= 0 || h <= 0 {
		return 0
	}
	switch {
	case w == 16 && h <= 128:
		return sad16Scalar(src, ss, ref, rs, h)
	case w == 8 && h <= 256:
		return sad8Scalar(src, ss, ref, rs, h)
	case w == 32 && h <= 64:
		return sad32Scalar(src, ss, ref, rs, h)
	case w == 64 && h <= 64:
		return sad64Scalar(src, ss, ref, rs, h)
	case w == 4:
		var s0, s1, s2, s3 int
		si, ri := 0, 0
		for range h {
			s := src[si : si+4]
			r := ref[ri : ri+4]
			_ = s[3]
			_ = r[3]
			d0 := int(s[0]) - int(r[0])
			d1 := int(s[1]) - int(r[1])
			d2 := int(s[2]) - int(r[2])
			d3 := int(s[3]) - int(r[3])
			s0 += max(d0, -d0)
			s1 += max(d1, -d1)
			s2 += max(d2, -d2)
			s3 += max(d3, -d3)
			si += ss
			ri += rs
		}
		return s0 + s1 + s2 + s3
	default:
		si, ri := 0, 0
		for range h {
			s := src[si : si+w]
			r := ref[ri : ri+w]
			for x := range w {
				d := int(s[x]) - int(r[x])
				sum += max(d, -d)
			}
			si += ss
			ri += rs
		}
		return
	}
}

func sad16Scalar(src []byte, ss int, ref []byte, rs, h int) int {
	const mask00FF = uint64(0x00ff00ff00ff00ff)
	if h <= 128 {
		var accLo, accHi uint64
		si, ri := 0, 0
		for range h {
			s := src[si : si+16]
			r := ref[ri : ri+16]
			a := binary.LittleEndian.Uint64(s[0:8])
			b := binary.LittleEndian.Uint64(r[0:8])
			accLo += sadSWAR4(a&mask00FF, b&mask00FF)
			accHi += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
			a = binary.LittleEndian.Uint64(s[8:16])
			b = binary.LittleEndian.Uint64(r[8:16])
			accLo += sadSWAR4(a&mask00FF, b&mask00FF)
			accHi += sadSWAR4((a>>8)&mask00FF, (b>>8)&mask00FF)
			si += ss
			ri += rs
		}
		return sumLanes16(accLo) + sumLanes16(accHi)
	}
	var sum int
	for h > 0 {
		chunk := min(h, 128)
		sum += sad16Scalar(src, ss, ref, rs, chunk)
		src = src[chunk*ss:]
		ref = ref[chunk*rs:]
		h -= chunk
	}
	return sum
}

func StatsScalar(src []byte, ss int, ref []byte, rs, w, h int) (sum int, sse uint32) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	switch w {
	case 16:
		var sum0, sum1, sse0, sse1, sse2, sse3 int
		si, ri := 0, 0
		for range h {
			s := (*[16]byte)(src[si : si+16])
			r := (*[16]byte)(ref[ri : ri+16])
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
			si += ss
			ri += rs
		}
		return sum0 + sum1, uint32(sse0 + sse1 + sse2 + sse3)
	case 32, 64:
		var sum0, sum1, sse0, sse1, sse2, sse3 int
		si, ri := 0, 0
		for range h {
			sRow := src[si : si+w]
			rRow := ref[ri : ri+w]
			for x := 0; x < w; x += 16 {
				s := (*[16]byte)(sRow[x : x+16])
				r := (*[16]byte)(rRow[x : x+16])
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
		return sum0 + sum1, uint32(sse0 + sse1 + sse2 + sse3)
	case 8:
		var sum0, sum1, sse0, sse1, sse2, sse3 int
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
			si += ss
			ri += rs
		}
		return sum0 + sum1, uint32(sse0 + sse1 + sse2 + sse3)
	case 4:
		var sum0, sse0, sse1, sse2, sse3 int
		si, ri := 0, 0
		for range h {
			s := src[si : si+4]
			r := ref[ri : ri+4]
			_ = s[3]
			_ = r[3]
			d0 := int(s[0]) - int(r[0])
			d1 := int(s[1]) - int(r[1])
			d2 := int(s[2]) - int(r[2])
			d3 := int(s[3]) - int(r[3])
			sum0 += d0 + d1 + d2 + d3
			sse0 += d0 * d0
			sse1 += d1 * d1
			sse2 += d2 * d2
			sse3 += d3 * d3
			si += ss
			ri += rs
		}
		return sum0, uint32(sse0 + sse1 + sse2 + sse3)
	}
	if w&7 == 0 {
		var sum0, sum1, sse0, sse1, sse2, sse3 int
		si, ri := 0, 0
		for range h {
			s := src[si : si+w]
			r := ref[ri : ri+w]
			for x := 0; x < w; x += 8 {
				sx := s[x : x+8]
				rx := r[x : x+8]
				_ = sx[7]
				_ = rx[7]
				d0 := int(sx[0]) - int(rx[0])
				d1 := int(sx[1]) - int(rx[1])
				d2 := int(sx[2]) - int(rx[2])
				d3 := int(sx[3]) - int(rx[3])
				sum0 += d0 + d1 + d2 + d3
				sse0 += d0 * d0
				sse1 += d1 * d1
				sse2 += d2 * d2
				sse3 += d3 * d3
				d4 := int(sx[4]) - int(rx[4])
				d5 := int(sx[5]) - int(rx[5])
				d6 := int(sx[6]) - int(rx[6])
				d7 := int(sx[7]) - int(rx[7])
				sum1 += d4 + d5 + d6 + d7
				sse0 += d4 * d4
				sse1 += d5 * d5
				sse2 += d6 * d6
				sse3 += d7 * d7
			}
			si += ss
			ri += rs
		}
		return sum0 + sum1, uint32(sse0 + sse1 + sse2 + sse3)
	}
	si, ri := 0, 0
	for range h {
		s := src[si : si+w]
		r := ref[ri : ri+w]
		for x := range w {
			d := int(s[x]) - int(r[x])
			sum += d
			sse += uint32(d * d)
		}
		si += ss
		ri += rs
	}
	return
}

func SubtractScalar(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) (nonzero bool) {
	if w <= 0 || h <= 0 {
		return false
	}
	var nz int16
	si, pi, oi := 0, 0, 0
	if w&7 == 0 {
		for range h {
			s := src[si : si+w]
			p := pred[pi : pi+w]
			o := out[oi : oi+w]
			for x := 0; x < w; x += 8 {
				sx := (*[8]byte)(s[x : x+8])
				px := (*[8]byte)(p[x : x+8])
				ox := (*[8]int16)(o[x : x+8])
				d0 := int16(sx[0]) - int16(px[0])
				d1 := int16(sx[1]) - int16(px[1])
				d2 := int16(sx[2]) - int16(px[2])
				d3 := int16(sx[3]) - int16(px[3])
				d4 := int16(sx[4]) - int16(px[4])
				d5 := int16(sx[5]) - int16(px[5])
				d6 := int16(sx[6]) - int16(px[6])
				d7 := int16(sx[7]) - int16(px[7])
				ox[0] = d0
				ox[1] = d1
				ox[2] = d2
				ox[3] = d3
				ox[4] = d4
				ox[5] = d5
				ox[6] = d6
				ox[7] = d7
				nz |= d0 | d1 | d2 | d3 | d4 | d5 | d6 | d7
			}
			si += ss
			pi += ps
			oi += os
		}
		return nz != 0
	}
	if w == 4 {
		for range h {
			s := src[si : si+4]
			p := pred[pi : pi+4]
			o := out[oi : oi+4]
			_ = s[3]
			_ = p[3]
			_ = o[3]
			d0 := int16(s[0]) - int16(p[0])
			d1 := int16(s[1]) - int16(p[1])
			d2 := int16(s[2]) - int16(p[2])
			d3 := int16(s[3]) - int16(p[3])
			o[0] = d0
			o[1] = d1
			o[2] = d2
			o[3] = d3
			nz |= d0 | d1 | d2 | d3
			si += ss
			pi += ps
			oi += os
		}
		return nz != 0
	}
	for range h {
		s := src[si : si+w]
		p := pred[pi : pi+w]
		o := out[oi : oi+w]
		for x := range w {
			d := int16(s[x]) - int16(p[x])
			o[x] = d
			nz |= d
		}
		si += ss
		pi += ps
		oi += os
	}
	return nz != 0
}

func filter2HorizSWAR(src []byte, ss int, dst []byte, ds, w, h, t0, t1 int) {
	const (
		maskFE   = uint64(0xfefefefefefefefe)
		mask00FF = uint64(0x00ff00ff00ff00ff)
		maskFF00 = uint64(0xff00ff00ff00ff00)
		bias64   = uint64(0x0040004000400040)
	)
	si, di := 0, 0
	if t0 == 64 && t1 == 64 {
		for range h {
			s := src[si : si+w+1]
			d := dst[di : di+w]
			x := 0
			for ; x+7 < w; x += 8 {
				a := binary.LittleEndian.Uint64(s[x:])
				b := binary.LittleEndian.Uint64(s[x+1:])
				binary.LittleEndian.PutUint64(d[x:], (a|b)-(((a^b)&maskFE)>>1))
			}
			for ; x < w; x++ {
				d[x] = byte((int(s[x]) + int(s[x+1]) + 1) >> 1)
			}
			si += ss
			di += ds
		}
		return
	}
	f0, f1 := uint64(t0), uint64(t1)
	for range h {
		s := src[si : si+w+1]
		d := dst[di : di+w]
		x := 0
		for ; x+7 < w; x += 8 {
			w0 := binary.LittleEndian.Uint64(s[x:])
			w1 := binary.LittleEndian.Uint64(s[x+1:])
			lo := (((w0&mask00FF)*f0 + (w1&mask00FF)*f1 + bias64) >> 7) & mask00FF
			hi := ((((w0>>8)&mask00FF)*f0 + ((w1>>8)&mask00FF)*f1 + bias64) << 1) & maskFF00
			binary.LittleEndian.PutUint64(d[x:], lo|hi)
		}
		for ; x < w; x++ {
			d[x] = byte((int(s[x])*t0 + int(s[x+1])*t1 + 64) >> 7)
		}
		si += ss
		di += ds
	}
}

func filter2VertSWAR(src []byte, ss int, dst []byte, ds, w, h, step, t0, t1 int) {
	const (
		maskFE   = uint64(0xfefefefefefefefe)
		mask00FF = uint64(0x00ff00ff00ff00ff)
		maskFF00 = uint64(0xff00ff00ff00ff00)
		bias64   = uint64(0x0040004000400040)
	)
	si, di := 0, 0
	if t0 == 64 && t1 == 64 {
		for range h {
			r0 := src[si : si+w]
			r1 := src[si+step : si+step+w]
			d := dst[di : di+w]
			x := 0
			for ; x+7 < w; x += 8 {
				a := binary.LittleEndian.Uint64(r0[x:])
				b := binary.LittleEndian.Uint64(r1[x:])
				binary.LittleEndian.PutUint64(d[x:], (a|b)-(((a^b)&maskFE)>>1))
			}
			for ; x < w; x++ {
				d[x] = byte((int(r0[x]) + int(r1[x]) + 1) >> 1)
			}
			si += ss
			di += ds
		}
		return
	}
	f0, f1 := uint64(t0), uint64(t1)
	for range h {
		r0 := src[si : si+w]
		r1 := src[si+step : si+step+w]
		d := dst[di : di+w]
		x := 0
		for ; x+7 < w; x += 8 {
			w0 := binary.LittleEndian.Uint64(r0[x:])
			w1 := binary.LittleEndian.Uint64(r1[x:])
			lo := (((w0&mask00FF)*f0 + (w1&mask00FF)*f1 + bias64) >> 7) & mask00FF
			hi := ((((w0>>8)&mask00FF)*f0 + ((w1>>8)&mask00FF)*f1 + bias64) << 1) & maskFF00
			binary.LittleEndian.PutUint64(d[x:], lo|hi)
		}
		for ; x < w; x++ {
			d[x] = byte((int(r0[x])*t0 + int(r1[x])*t1 + 64) >> 7)
		}
		si += ss
		di += ds
	}
}

func filter8HorizSWAR(src []byte, ss int, dst []byte, ds, w, h, t0, t1, t2, t3, t4, t5, t6, t7 int) {
	const (
		mask00FF = uint64(0x00ff00ff00ff00ff)
		bias4040 = uint64(0x4040404040404040)
	)
	n0, p1, n2, p3 := uint64(-t0), uint64(t1), uint64(-t2), uint64(t3)
	p4, n5, p6, n7 := uint64(t4), uint64(-t5), uint64(t6), uint64(-t7)
	lut := &filterClipLUT
	si, di := 0, 0
	for range h {
		s := src[si : si+w+7]
		d := dst[di : di+w]
		x := 0
		for ; x+7 < w; x += 8 {
			w0 := binary.LittleEndian.Uint64(s[x:])
			w1 := binary.LittleEndian.Uint64(s[x+1:])
			w2 := binary.LittleEndian.Uint64(s[x+2:])
			w3 := binary.LittleEndian.Uint64(s[x+3:])
			w4 := binary.LittleEndian.Uint64(s[x+4:])
			w5 := binary.LittleEndian.Uint64(s[x+5:])
			w6 := binary.LittleEndian.Uint64(s[x+6:])
			w7 := binary.LittleEndian.Uint64(s[x+7:])
			accLo := bias4040 + (w1&mask00FF)*p1 + (w3&mask00FF)*p3 + (w4&mask00FF)*p4 + (w6&mask00FF)*p6 -
				(w0&mask00FF)*n0 - (w2&mask00FF)*n2 - (w5&mask00FF)*n5 - (w7&mask00FF)*n7
			accHi := bias4040 + ((w1>>8)&mask00FF)*p1 + ((w3>>8)&mask00FF)*p3 + ((w4>>8)&mask00FF)*p4 + ((w6>>8)&mask00FF)*p6 -
				((w0>>8)&mask00FF)*n0 - ((w2>>8)&mask00FF)*n2 - ((w5>>8)&mask00FF)*n5 - ((w7>>8)&mask00FF)*n7
			out := uint64(lut[(accLo>>7)&0x1ff]) |
				uint64(lut[(accHi>>7)&0x1ff])<<8 |
				uint64(lut[(accLo>>23)&0x1ff])<<16 |
				uint64(lut[(accHi>>23)&0x1ff])<<24 |
				uint64(lut[(accLo>>39)&0x1ff])<<32 |
				uint64(lut[(accHi>>39)&0x1ff])<<40 |
				uint64(lut[(accLo>>55)&0x1ff])<<48 |
				uint64(lut[(accHi>>55)&0x1ff])<<56
			binary.LittleEndian.PutUint64(d[x:], out)
		}
		for ; x < w; x++ {
			v := int(s[x])*t0 + int(s[x+1])*t1 + int(s[x+2])*t2 + int(s[x+3])*t3 +
				int(s[x+4])*t4 + int(s[x+5])*t5 + int(s[x+6])*t6 + int(s[x+7])*t7 + 64
			d[x] = byte(min(max(v>>7, 0), 255))
		}
		si += ss
		di += ds
	}
}

func filter8VertSWAR(src []byte, ss int, dst []byte, ds, w, h, step, t0, t1, t2, t3, t4, t5, t6, t7 int) {
	const (
		mask00FF = uint64(0x00ff00ff00ff00ff)
		bias4040 = uint64(0x4040404040404040)
	)
	n0, p1, n2, p3 := uint64(-t0), uint64(t1), uint64(-t2), uint64(t3)
	p4, n5, p6, n7 := uint64(t4), uint64(-t5), uint64(t6), uint64(-t7)
	lut := &filterClipLUT
	si, di := 0, 0
	s1, s2, s3, s4, s5, s6, s7 := step, 2*step, 3*step, 4*step, 5*step, 6*step, 7*step
	for range h {
		r0 := src[si : si+w]
		r1 := src[si+s1 : si+s1+w]
		r2 := src[si+s2 : si+s2+w]
		r3 := src[si+s3 : si+s3+w]
		r4 := src[si+s4 : si+s4+w]
		r5 := src[si+s5 : si+s5+w]
		r6 := src[si+s6 : si+s6+w]
		r7 := src[si+s7 : si+s7+w]
		d := dst[di : di+w]
		x := 0
		for ; x+7 < w; x += 8 {
			w0 := binary.LittleEndian.Uint64(r0[x:])
			w1 := binary.LittleEndian.Uint64(r1[x:])
			w2 := binary.LittleEndian.Uint64(r2[x:])
			w3 := binary.LittleEndian.Uint64(r3[x:])
			w4 := binary.LittleEndian.Uint64(r4[x:])
			w5 := binary.LittleEndian.Uint64(r5[x:])
			w6 := binary.LittleEndian.Uint64(r6[x:])
			w7 := binary.LittleEndian.Uint64(r7[x:])
			accLo := bias4040 + (w1&mask00FF)*p1 + (w3&mask00FF)*p3 + (w4&mask00FF)*p4 + (w6&mask00FF)*p6 -
				(w0&mask00FF)*n0 - (w2&mask00FF)*n2 - (w5&mask00FF)*n5 - (w7&mask00FF)*n7
			accHi := bias4040 + ((w1>>8)&mask00FF)*p1 + ((w3>>8)&mask00FF)*p3 + ((w4>>8)&mask00FF)*p4 + ((w6>>8)&mask00FF)*p6 -
				((w0>>8)&mask00FF)*n0 - ((w2>>8)&mask00FF)*n2 - ((w5>>8)&mask00FF)*n5 - ((w7>>8)&mask00FF)*n7
			out := uint64(lut[(accLo>>7)&0x1ff]) |
				uint64(lut[(accHi>>7)&0x1ff])<<8 |
				uint64(lut[(accLo>>23)&0x1ff])<<16 |
				uint64(lut[(accHi>>23)&0x1ff])<<24 |
				uint64(lut[(accLo>>39)&0x1ff])<<32 |
				uint64(lut[(accHi>>39)&0x1ff])<<40 |
				uint64(lut[(accLo>>55)&0x1ff])<<48 |
				uint64(lut[(accHi>>55)&0x1ff])<<56
			binary.LittleEndian.PutUint64(d[x:], out)
		}
		for ; x < w; x++ {
			v := int(r0[x])*t0 + int(r1[x])*t1 + int(r2[x])*t2 + int(r3[x])*t3 +
				int(r4[x])*t4 + int(r5[x])*t5 + int(r6[x])*t6 + int(r7[x])*t7 + 64
			d[x] = byte(min(max(v>>7, 0), 255))
		}
		si += ss
		di += ds
	}
}

func FilterScalar(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) {
	if w <= 0 || h <= 0 {
		return
	}
	switch len(taps) {
	case 8:
		t0, t1, t2, t3 := int(taps[0]), int(taps[1]), int(taps[2]), int(taps[3])
		t4, t5, t6, t7 := int(taps[4]), int(taps[5]), int(taps[6]), int(taps[7])
		if step == 1 {
			if t0|t1|t2|t5|t6|t7 == 0 {
				if t3 >= 0 && t4 >= 0 && t3+t4 == 128 && w >= 8 {
					filter2HorizSWAR(src[3:], ss, dst, ds, w, h, t3, t4)
					return
				}
				si, di := 3, 0
				for range h {
					s := src[si : si+w+1]
					d := dst[di : di+w]
					x := 0
					for ; x+3 < w; x += 4 {
						s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
						d[x] = byte(min(max((s0*t3+s1*t4+64)>>7, 0), 255))
						d[x+1] = byte(min(max((s1*t3+s2*t4+64)>>7, 0), 255))
						d[x+2] = byte(min(max((s2*t3+s3*t4+64)>>7, 0), 255))
						d[x+3] = byte(min(max((s3*t3+s4*t4+64)>>7, 0), 255))
					}
					for ; x < w; x++ {
						d[x] = byte(min(max((int(s[x])*t3+int(s[x+1])*t4+64)>>7, 0), 255))
					}
					si += ss
					di += ds
				}
				return
			}
			if w >= 8 && t0 <= 0 && t1 >= 0 && t2 <= 0 && t3 >= 0 && t4 >= 0 && t5 <= 0 && t6 >= 0 && t7 <= 0 &&
				(t1+t3+t4+t6)*255 <= 49087 && -(t0+t2+t5+t7)*255 <= 16448 {
				filter8HorizSWAR(src, ss, dst, ds, w, h, t0, t1, t2, t3, t4, t5, t6, t7)
				return
			}
			si, di := 0, 0
			for range h {
				s := src[si : si+w+7]
				d := dst[di : di+w]
				for x := range w {
					v := int(s[x])*t0 + int(s[x+1])*t1 + int(s[x+2])*t2 + int(s[x+3])*t3 +
						int(s[x+4])*t4 + int(s[x+5])*t5 + int(s[x+6])*t6 + int(s[x+7])*t7 + 64
					d[x] = byte(min(max(v>>7, 0), 255))
				}
				si += ss
				di += ds
			}
			return
		}
		if t0|t1|t2|t5|t6|t7 == 0 {
			if t3 >= 0 && t4 >= 0 && t3+t4 == 128 && w >= 8 {
				filter2VertSWAR(src[3*step:], ss, dst, ds, w, h, step, t3, t4)
				return
			}
			si, di := 3*step, 0
			for range h {
				r0 := src[si : si+w]
				r1 := src[si+step : si+step+w]
				d := dst[di : di+w]
				for x := range w {
					d[x] = byte(min(max((int(r0[x])*t3+int(r1[x])*t4+64)>>7, 0), 255))
				}
				si += ss
				di += ds
			}
			return
		}
		if w >= 8 && t0 <= 0 && t1 >= 0 && t2 <= 0 && t3 >= 0 && t4 >= 0 && t5 <= 0 && t6 >= 0 && t7 <= 0 &&
			(t1+t3+t4+t6)*255 <= 49087 && -(t0+t2+t5+t7)*255 <= 16448 {
			filter8VertSWAR(src, ss, dst, ds, w, h, step, t0, t1, t2, t3, t4, t5, t6, t7)
			return
		}
		si, di := 0, 0
		s1, s2, s3, s4, s5, s6, s7 := step, 2*step, 3*step, 4*step, 5*step, 6*step, 7*step
		for range h {
			r0 := src[si : si+w]
			r1 := src[si+s1 : si+s1+w]
			r2 := src[si+s2 : si+s2+w]
			r3 := src[si+s3 : si+s3+w]
			r4 := src[si+s4 : si+s4+w]
			r5 := src[si+s5 : si+s5+w]
			r6 := src[si+s6 : si+s6+w]
			r7 := src[si+s7 : si+s7+w]
			d := dst[di : di+w]
			for x := range w {
				v := int(r0[x])*t0 + int(r1[x])*t1 + int(r2[x])*t2 + int(r3[x])*t3 +
					int(r4[x])*t4 + int(r5[x])*t5 + int(r6[x])*t6 + int(r7[x])*t7 + 64
				d[x] = byte(min(max(v>>7, 0), 255))
			}
			si += ss
			di += ds
		}
		return
	case 2:
		t0, t1 := int(taps[0]), int(taps[1])
		if step == 1 {
			if t0 >= 0 && t1 >= 0 && t0+t1 == 128 && w >= 8 {
				filter2HorizSWAR(src, ss, dst, ds, w, h, t0, t1)
				return
			}
			si, di := 0, 0
			for range h {
				s := src[si : si+w+1]
				d := dst[di : di+w]
				for x := range w {
					d[x] = byte(min(max((int(s[x])*t0+int(s[x+1])*t1+64)>>7, 0), 255))
				}
				si += ss
				di += ds
			}
			return
		}
		if t0 >= 0 && t1 >= 0 && t0+t1 == 128 && w >= 8 {
			filter2VertSWAR(src, ss, dst, ds, w, h, step, t0, t1)
			return
		}
		si, di := 0, 0
		for range h {
			r0 := src[si : si+w]
			r1 := src[si+step : si+step+w]
			d := dst[di : di+w]
			for x := range w {
				d[x] = byte(min(max((int(r0[x])*t0+int(r1[x])*t1+64)>>7, 0), 255))
			}
			si += ss
			di += ds
		}
		return
	case 6:
		t0, t1, t2, t3, t4, t5 := int(taps[0]), int(taps[1]), int(taps[2]), int(taps[3]), int(taps[4]), int(taps[5])
		si, di := 0, 0
		if step == 1 {
			for range h {
				s := src[si : si+w+5]
				d := dst[di : di+w]
				x := 0
				for ; x+3 < w; x += 4 {
					s0, s1, s2, s3, s4, s5 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4]), int(s[x+5])
					s6, s7, s8 := int(s[x+6]), int(s[x+7]), int(s[x+8])
					d[x] = byte(min(max((s0*t0+s1*t1+s2*t2+s3*t3+s4*t4+s5*t5+64)>>7, 0), 255))
					d[x+1] = byte(min(max((s1*t0+s2*t1+s3*t2+s4*t3+s5*t4+s6*t5+64)>>7, 0), 255))
					d[x+2] = byte(min(max((s2*t0+s3*t1+s4*t2+s5*t3+s6*t4+s7*t5+64)>>7, 0), 255))
					d[x+3] = byte(min(max((s3*t0+s4*t1+s5*t2+s6*t3+s7*t4+s8*t5+64)>>7, 0), 255))
				}
				for ; x < w; x++ {
					v := int(s[x])*t0 + int(s[x+1])*t1 + int(s[x+2])*t2 + int(s[x+3])*t3 + int(s[x+4])*t4 + int(s[x+5])*t5 + 64
					d[x] = byte(min(max(v>>7, 0), 255))
				}
				si += ss
				di += ds
			}
			return
		}
		s1, s2, s3, s4, s5 := step, 2*step, 3*step, 4*step, 5*step
		for range h {
			r0 := src[si : si+w]
			r1 := src[si+s1 : si+s1+w]
			r2 := src[si+s2 : si+s2+w]
			r3 := src[si+s3 : si+s3+w]
			r4 := src[si+s4 : si+s4+w]
			r5 := src[si+s5 : si+s5+w]
			d := dst[di : di+w]
			for x := range w {
				v := int(r0[x])*t0 + int(r1[x])*t1 + int(r2[x])*t2 + int(r3[x])*t3 + int(r4[x])*t4 + int(r5[x])*t5 + 64
				d[x] = byte(min(max(v>>7, 0), 255))
			}
			si += ss
			di += ds
		}
		return
	}
	for y := range h {
		for x := range w {
			v := 64
			for k, c := range taps {
				v += int(src[y*ss+x+k*step]) * int(c)
			}
			dst[y*ds+x] = byte(min(max(v>>7, 0), 255))
		}
	}
}

func AverageScalar(src []byte, ss int, dst []byte, ds, w, h int) {
	if w <= 0 || h <= 0 {
		return
	}
	const maskFE = uint64(0xfefefefefefefefe)
	si, di := 0, 0
	for range h {
		s := src[si : si+w]
		d := dst[di : di+w]
		x := 0
		for ; x+7 < w; x += 8 {
			a := binary.LittleEndian.Uint64(s[x:])
			b := binary.LittleEndian.Uint64(d[x:])
			avg := (a | b) - (((a ^ b) & maskFE) >> 1)
			binary.LittleEndian.PutUint64(d[x:], avg)
		}
		for ; x < w; x++ {
			d[x] = byte((int(s[x]) + int(d[x]) + 1) >> 1)
		}
		si += ss
		di += ds
	}
}

func ErrorScalar(a, b []int16, n int) (sum uint64) {
	if n <= 0 {
		return 0
	}
	a = a[:n]
	b = b[:n]
	var s0, s1, s2, s3 uint64
	i := 0
	for ; i+3 < n; i += 4 {
		d0 := int64(a[i]) - int64(b[i])
		d1 := int64(a[i+1]) - int64(b[i+1])
		d2 := int64(a[i+2]) - int64(b[i+2])
		d3 := int64(a[i+3]) - int64(b[i+3])
		s0 += uint64(d0 * d0)
		s1 += uint64(d1 * d1)
		s2 += uint64(d2 * d2)
		s3 += uint64(d3 * d3)
	}
	sum = s0 + s1 + s2 + s3
	for ; i < n; i++ {
		d := int64(a[i]) - int64(b[i])
		sum += uint64(d * d)
	}
	return
}

func QuantizeScalar(coeff, iscan, qcoeff, dqcoeff []int16, n, zbin, round, quant, shift, dequant int, fp bool) (eob int) {
	if n <= 0 {
		return 0
	}
	coeff = coeff[:n]
	iscan = iscan[:n]
	dqcoeff = dqcoeff[:n]
	hasQ := qcoeff != nil
	if hasQ {
		qcoeff = qcoeff[:n]
	}
	if fp {
		thresh := dequant - round
		for i := range n {
			c := int(coeff[i])
			a := max(c, -c)
			if a >= thresh {
				v := min(a+round, 32767)
				q := v * quant >> 16
				if c < 0 {
					q = -q
				}
				if hasQ {
					qcoeff[i] = int16(q)
				}
				dqcoeff[i] = int16(q * dequant)
				if q != 0 {
					eob = max(eob, int(iscan[i]))
				}
			} else {
				if hasQ {
					qcoeff[i] = 0
				}
				dqcoeff[i] = 0
			}
		}
		return
	}
	for i := range n {
		c := int(coeff[i])
		a := max(c, -c)
		if a >= zbin {
			v := min(max(a+round, -32768), 32767)
			q := (((v * quant >> 16) + v) * shift) >> 16
			if c < 0 {
				q = -q
			}
			if hasQ {
				qcoeff[i] = int16(q)
			}
			dqcoeff[i] = int16(q * dequant)
			if q != 0 {
				eob = max(eob, int(iscan[i]))
			}
		} else {
			if hasQ {
				qcoeff[i] = 0
			}
			dqcoeff[i] = 0
		}
	}
	return
}

func GatherScalar(src []byte, ss int, pred []byte, ps int, out []int16, w int) {
	if w <= 0 {
		return
	}
	bw := w / 4
	for by := range bw {
		for bx := range bw {
			off := (by*bw + bx) * 16
			o := out[off : off+16]
			_ = o[15]
			si := by*4*ss + bx*4
			pi := by*4*ps + bx*4
			for r := range 4 {
				sr := src[si : si+4]
				pr := pred[pi : pi+4]
				_ = sr[3]
				_ = pr[3]
				or := o[r*4 : r*4+4]
				or[0] = int16(sr[0]) - int16(pr[0])
				or[1] = int16(sr[1]) - int16(pr[1])
				or[2] = int16(sr[2]) - int16(pr[2])
				or[3] = int16(sr[3]) - int16(pr[3])
				si += ss
				pi += ps
			}
		}
	}
}

func TemporalScalar(src []byte, ss int, pred []byte, ps, w, strength, weight int, acc, count []uint32) {
	if w <= 0 {
		return
	}
	r := 0
	if strength > 0 {
		r = 1 << (strength - 1)
	}
	sh := uint(strength)
	si, pi, ai := 0, 0, 0
	for range w {
		sr := src[si : si+w]
		pr := pred[pi : pi+w]
		ac := acc[ai : ai+w]
		cn := count[ai : ai+w]
		for x := range w {
			pv := int(pr[x])
			d := int(sr[x]) - pv
			m := uint32((16 - min((d*d*3+r)>>sh, 16)) * weight)
			cn[x] += m
			ac[x] += m * uint32(pv)
		}
		si += ss
		pi += ps
		ai += w
	}
}

func VP8QuantScalar(c, r, q, d, qout, dout []int16) {
	_ = c[15]
	_ = r[15]
	_ = q[15]
	_ = d[15]
	_ = qout[15]
	_ = dout[15]
	for i := range 16 {
		v := int(c[i])
		a := max(v, -v)
		t := (a + int(r[i])) * int(q[i]) >> 16
		if v < 0 {
			t = -t
		}
		qout[i] = int16(t)
		dout[i] = int16(t * int(d[i]))
	}
}

func AddConstantScalar(pred []byte, ps int, dst []byte, ds, w, h, delta int) {
	if w <= 0 || h <= 0 {
		return
	}
	if delta == 0 {
		if &pred[0] != &dst[0] || ps != ds {
			pi, di := 0, 0
			for range h {
				copy(dst[di:di+w], pred[pi:pi+w])
				pi += ps
				di += ds
			}
		}
		return
	}
	pi, di := 0, 0
	if w&7 == 0 {
		for range h {
			p := pred[pi : pi+w]
			d := dst[di : di+w]
			for x := 0; x < w; x += 8 {
				px := p[x : x+8]
				dx := d[x : x+8]
				_ = px[7]
				_ = dx[7]
				dx[0] = byte(min(max(int(px[0])+delta, 0), 255))
				dx[1] = byte(min(max(int(px[1])+delta, 0), 255))
				dx[2] = byte(min(max(int(px[2])+delta, 0), 255))
				dx[3] = byte(min(max(int(px[3])+delta, 0), 255))
				dx[4] = byte(min(max(int(px[4])+delta, 0), 255))
				dx[5] = byte(min(max(int(px[5])+delta, 0), 255))
				dx[6] = byte(min(max(int(px[6])+delta, 0), 255))
				dx[7] = byte(min(max(int(px[7])+delta, 0), 255))
			}
			pi += ps
			di += ds
		}
		return
	}
	if w == 4 {
		for range h {
			p := pred[pi : pi+4]
			d := dst[di : di+4]
			_ = p[3]
			_ = d[3]
			d[0] = byte(min(max(int(p[0])+delta, 0), 255))
			d[1] = byte(min(max(int(p[1])+delta, 0), 255))
			d[2] = byte(min(max(int(p[2])+delta, 0), 255))
			d[3] = byte(min(max(int(p[3])+delta, 0), 255))
			pi += ps
			di += ds
		}
		return
	}
	for range h {
		p := pred[pi : pi+w]
		d := dst[di : di+w]
		for x := range w {
			d[x] = byte(min(max(int(p[x])+delta, 0), 255))
		}
		pi += ps
		di += ds
	}
}

func TMPredictScalar(dst []byte, ds int, above, left []byte, top byte, w int) {
	for y := range w {
		for x := range w {
			dst[y*ds+x] = byte(min(max(int(above[x])+int(left[y])-int(top), 0), 255))
		}
	}
}

func SubpelStatsScalar(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (sum int, sse uint32) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	fx0, fx1 := int(fx[0]), int(fx[1])
	fy0, fy1 := int(fy[0]), int(fy[1])
	if fy1 == 0 && fy0 == 128 {
		if fx1 == 0 && fx0 == 128 {
			return StatsScalar(src, ss, ref, rs, w, h)
		}
		if fx0 == 64 && fx1 == 64 && w == 16 {
			var sum0, sum1, sse0, sse1, sse2, sse3 int
			si, ri := 0, 0
			for range h {
				s := (*[17]byte)(src[si : si+17])
				r := (*[16]byte)(ref[ri : ri+16])
				s0, s1, s2, s3, s4 := int(s[0]), int(s[1]), int(s[2]), int(s[3]), int(s[4])
				d0 := ((s0 + s1 + 1) >> 1) - int(r[0])
				d1 := ((s1 + s2 + 1) >> 1) - int(r[1])
				d2 := ((s2 + s3 + 1) >> 1) - int(r[2])
				d3 := ((s3 + s4 + 1) >> 1) - int(r[3])
				sum0 += d0 + d1 + d2 + d3
				sse0 += d0 * d0
				sse1 += d1 * d1
				sse2 += d2 * d2
				sse3 += d3 * d3
				s5, s6, s7, s8 := int(s[5]), int(s[6]), int(s[7]), int(s[8])
				d4 := ((s4 + s5 + 1) >> 1) - int(r[4])
				d5 := ((s5 + s6 + 1) >> 1) - int(r[5])
				d6 := ((s6 + s7 + 1) >> 1) - int(r[6])
				d7 := ((s7 + s8 + 1) >> 1) - int(r[7])
				sum1 += d4 + d5 + d6 + d7
				sse0 += d4 * d4
				sse1 += d5 * d5
				sse2 += d6 * d6
				sse3 += d7 * d7
				s9, s10, s11, s12 := int(s[9]), int(s[10]), int(s[11]), int(s[12])
				d8 := ((s8 + s9 + 1) >> 1) - int(r[8])
				d9 := ((s9 + s10 + 1) >> 1) - int(r[9])
				d10 := ((s10 + s11 + 1) >> 1) - int(r[10])
				d11 := ((s11 + s12 + 1) >> 1) - int(r[11])
				sum0 += d8 + d9 + d10 + d11
				sse0 += d8 * d8
				sse1 += d9 * d9
				sse2 += d10 * d10
				sse3 += d11 * d11
				s13, s14, s15, s16 := int(s[13]), int(s[14]), int(s[15]), int(s[16])
				d12 := ((s12 + s13 + 1) >> 1) - int(r[12])
				d13 := ((s13 + s14 + 1) >> 1) - int(r[13])
				d14 := ((s14 + s15 + 1) >> 1) - int(r[14])
				d15 := ((s15 + s16 + 1) >> 1) - int(r[15])
				sum1 += d12 + d13 + d14 + d15
				sse0 += d12 * d12
				sse1 += d13 * d13
				sse2 += d14 * d14
				sse3 += d15 * d15
				si += ss
				ri += rs
			}
			return sum0 + sum1, uint32(sse0 + sse1 + sse2 + sse3)
		}
		var sumAcc, sse0, sse1 int
		si, ri := 0, 0
		for range h {
			s := src[si : si+w+1]
			r := ref[ri : ri+w]
			x := 0
			for ; x+3 < w; x += 4 {
				s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
				d0 := ((s0*fx0 + s1*fx1 + 64) >> 7) - int(r[x])
				d1 := ((s1*fx0 + s2*fx1 + 64) >> 7) - int(r[x+1])
				d2 := ((s2*fx0 + s3*fx1 + 64) >> 7) - int(r[x+2])
				d3 := ((s3*fx0 + s4*fx1 + 64) >> 7) - int(r[x+3])
				sumAcc += d0 + d1 + d2 + d3
				sse0 += d0*d0 + d2*d2
				sse1 += d1*d1 + d3*d3
			}
			for ; x < w; x++ {
				d := ((int(s[x])*fx0 + int(s[x+1])*fx1 + 64) >> 7) - int(r[x])
				sumAcc += d
				sse0 += d * d
			}
			si += ss
			ri += rs
		}
		return sumAcc, uint32(sse0 + sse1)
	}
	if fx1 == 0 && fx0 == 128 {
		if fy0 == 64 && fy1 == 64 && w == 16 {
			var sum0, sum1, sse0, sse1, sse2, sse3 int
			si, ri := 0, 0
			for range h {
				s0 := (*[16]byte)(src[si : si+16])
				s1 := (*[16]byte)(src[si+ss : si+ss+16])
				r := (*[16]byte)(ref[ri : ri+16])
				d0 := ((int(s0[0]) + int(s1[0]) + 1) >> 1) - int(r[0])
				d1 := ((int(s0[1]) + int(s1[1]) + 1) >> 1) - int(r[1])
				d2 := ((int(s0[2]) + int(s1[2]) + 1) >> 1) - int(r[2])
				d3 := ((int(s0[3]) + int(s1[3]) + 1) >> 1) - int(r[3])
				sum0 += d0 + d1 + d2 + d3
				sse0 += d0 * d0
				sse1 += d1 * d1
				sse2 += d2 * d2
				sse3 += d3 * d3
				d4 := ((int(s0[4]) + int(s1[4]) + 1) >> 1) - int(r[4])
				d5 := ((int(s0[5]) + int(s1[5]) + 1) >> 1) - int(r[5])
				d6 := ((int(s0[6]) + int(s1[6]) + 1) >> 1) - int(r[6])
				d7 := ((int(s0[7]) + int(s1[7]) + 1) >> 1) - int(r[7])
				sum1 += d4 + d5 + d6 + d7
				sse0 += d4 * d4
				sse1 += d5 * d5
				sse2 += d6 * d6
				sse3 += d7 * d7
				d8 := ((int(s0[8]) + int(s1[8]) + 1) >> 1) - int(r[8])
				d9 := ((int(s0[9]) + int(s1[9]) + 1) >> 1) - int(r[9])
				d10 := ((int(s0[10]) + int(s1[10]) + 1) >> 1) - int(r[10])
				d11 := ((int(s0[11]) + int(s1[11]) + 1) >> 1) - int(r[11])
				sum0 += d8 + d9 + d10 + d11
				sse0 += d8 * d8
				sse1 += d9 * d9
				sse2 += d10 * d10
				sse3 += d11 * d11
				d12 := ((int(s0[12]) + int(s1[12]) + 1) >> 1) - int(r[12])
				d13 := ((int(s0[13]) + int(s1[13]) + 1) >> 1) - int(r[13])
				d14 := ((int(s0[14]) + int(s1[14]) + 1) >> 1) - int(r[14])
				d15 := ((int(s0[15]) + int(s1[15]) + 1) >> 1) - int(r[15])
				sum1 += d12 + d13 + d14 + d15
				sse0 += d12 * d12
				sse1 += d13 * d13
				sse2 += d14 * d14
				sse3 += d15 * d15
				si += ss
				ri += rs
			}
			return sum0 + sum1, uint32(sse0 + sse1 + sse2 + sse3)
		}
		var sumAcc, sse0, sse1 int
		si, ri := 0, 0
		for range h {
			s0 := src[si : si+w]
			s1 := src[si+ss : si+ss+w]
			r := ref[ri : ri+w]
			x := 0
			for ; x+3 < w; x += 4 {
				d0 := ((int(s0[x])*fy0 + int(s1[x])*fy1 + 64) >> 7) - int(r[x])
				d1 := ((int(s0[x+1])*fy0 + int(s1[x+1])*fy1 + 64) >> 7) - int(r[x+1])
				d2 := ((int(s0[x+2])*fy0 + int(s1[x+2])*fy1 + 64) >> 7) - int(r[x+2])
				d3 := ((int(s0[x+3])*fy0 + int(s1[x+3])*fy1 + 64) >> 7) - int(r[x+3])
				sumAcc += d0 + d1 + d2 + d3
				sse0 += d0*d0 + d2*d2
				sse1 += d1*d1 + d3*d3
			}
			for ; x < w; x++ {
				d := ((int(s0[x])*fy0 + int(s1[x])*fy1 + 64) >> 7) - int(r[x])
				sumAcc += d
				sse0 += d * d
			}
			si += ss
			ri += rs
		}
		return sumAcc, uint32(sse0 + sse1)
	}
	var prevBuf [64]uint16
	var prev []uint16
	if w <= len(prevBuf) {
		prev = prevBuf[:w]
	} else {
		prev = make([]uint16, w)
	}
	s0 := src[:w+1]
	for x := range w {
		prev[x] = uint16((int(s0[x])*fx0 + int(s0[x+1])*fx1 + 64) >> 7)
	}
	si, ri := ss, 0
	var sumAcc, sse0, sse1 int
	for range h {
		s := src[si : si+w+1]
		r := ref[ri : ri+w]
		x := 0
		for ; x+3 < w; x += 4 {
			s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
			b0 := (s0*fx0 + s1*fx1 + 64) >> 7
			b1 := (s1*fx0 + s2*fx1 + 64) >> 7
			b2 := (s2*fx0 + s3*fx1 + 64) >> 7
			b3 := (s3*fx0 + s4*fx1 + 64) >> 7
			a0, a1, a2, a3 := int(prev[x]), int(prev[x+1]), int(prev[x+2]), int(prev[x+3])
			prev[x], prev[x+1], prev[x+2], prev[x+3] = uint16(b0), uint16(b1), uint16(b2), uint16(b3)
			d0 := ((a0*fy0 + b0*fy1 + 64) >> 7) - int(r[x])
			d1 := ((a1*fy0 + b1*fy1 + 64) >> 7) - int(r[x+1])
			d2 := ((a2*fy0 + b2*fy1 + 64) >> 7) - int(r[x+2])
			d3 := ((a3*fy0 + b3*fy1 + 64) >> 7) - int(r[x+3])
			sumAcc += d0 + d1 + d2 + d3
			sse0 += d0*d0 + d2*d2
			sse1 += d1*d1 + d3*d3
		}
		for ; x < w; x++ {
			a := int(prev[x])
			b := (int(s[x])*fx0 + int(s[x+1])*fx1 + 64) >> 7
			prev[x] = uint16(b)
			d := ((a*fy0 + b*fy1 + 64) >> 7) - int(r[x])
			sumAcc += d
			sse0 += d * d
		}
		si += ss
		ri += rs
	}
	return sumAcc, uint32(sse0 + sse1)
}

func BilinearFirstScalar(src []byte, ss int, dst []uint16, w, h int, f [2]int16) {
	if w <= 0 || h <= 0 {
		return
	}
	f0, f1 := int(f[0]), int(f[1])
	si, di := 0, 0
	for range h {
		s := src[si : si+w+1]
		d := dst[di : di+w]
		x := 0
		for ; x+3 < w; x += 4 {
			s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
			d[x] = uint16((s0*f0 + s1*f1 + 64) >> 7)
			d[x+1] = uint16((s1*f0 + s2*f1 + 64) >> 7)
			d[x+2] = uint16((s2*f0 + s3*f1 + 64) >> 7)
			d[x+3] = uint16((s3*f0 + s4*f1 + 64) >> 7)
		}
		for ; x < w; x++ {
			d[x] = uint16((int(s[x])*f0 + int(s[x+1])*f1 + 64) >> 7)
		}
		si += ss
		di += w
	}
}

func BilinearSecondScalar(src []uint16, dst []byte, w, h int, f [2]int16) {
	if w <= 0 || h <= 0 {
		return
	}
	f0, f1 := int(f[0]), int(f[1])
	si := 0
	for range h {
		r0 := src[si : si+w]
		r1 := src[si+w : si+2*w]
		d := dst[si : si+w]
		x := 0
		for ; x+3 < w; x += 4 {
			d[x] = byte((int(r0[x])*f0 + int(r1[x])*f1 + 64) >> 7)
			d[x+1] = byte((int(r0[x+1])*f0 + int(r1[x+1])*f1 + 64) >> 7)
			d[x+2] = byte((int(r0[x+2])*f0 + int(r1[x+2])*f1 + 64) >> 7)
			d[x+3] = byte((int(r0[x+3])*f0 + int(r1[x+3])*f1 + 64) >> 7)
		}
		for ; x < w; x++ {
			d[x] = byte((int(r0[x])*f0 + int(r1[x])*f1 + 64) >> 7)
		}
		si += w
	}
}

func SumScalar(src []byte, ss, w, h int) (sum int) {
	if w <= 0 || h <= 0 {
		return 0
	}
	var s0, s1, s2, s3 int
	si := 0
	for range h {
		s := src[si : si+w]
		x := 0
		for ; x+7 < w; x += 8 {
			sx := s[x : x+8]
			_ = sx[7]
			s0 += int(sx[0]) + int(sx[4])
			s1 += int(sx[1]) + int(sx[5])
			s2 += int(sx[2]) + int(sx[6])
			s3 += int(sx[3]) + int(sx[7])
		}
		for ; x < w; x++ {
			s0 += int(s[x])
		}
		si += ss
	}
	return s0 + s1 + s2 + s3
}

func EnergyScalar(src []int16) (sum uint64) {
	n := len(src)
	var s0, s1, s2, s3 uint64
	i := 0
	for ; i+3 < n; i += 4 {
		v0 := int64(src[i])
		v1 := int64(src[i+1])
		v2 := int64(src[i+2])
		v3 := int64(src[i+3])
		s0 += uint64(v0 * v0)
		s1 += uint64(v1 * v1)
		s2 += uint64(v2 * v2)
		s3 += uint64(v3 * v3)
	}
	sum = s0 + s1 + s2 + s3
	for ; i < n; i++ {
		v := int64(src[i])
		sum += uint64(v * v)
	}
	return
}

func AbsSumScalar(src []int16, n int) (sum int) {
	if n <= 0 {
		return 0
	}
	s := src[:n]
	var s0, s1, s2, s3 int
	i := 0
	for ; i+3 < n; i += 4 {
		v0, v1, v2, v3 := int(s[i]), int(s[i+1]), int(s[i+2]), int(s[i+3])
		s0 += max(v0, -v0)
		s1 += max(v1, -v1)
		s2 += max(v2, -v2)
		s3 += max(v3, -v3)
	}
	sum = s0 + s1 + s2 + s3
	for ; i < n; i++ {
		v := int(s[i])
		sum += max(v, -v)
	}
	return
}

func ProjectionRowScalar(src []byte, ss, h int, out []int16, shift uint64) {
	var acc [16]int
	si := 0
	for range h {
		s := src[si : si+16]
		_ = s[15]
		acc[0] += int(s[0])
		acc[1] += int(s[1])
		acc[2] += int(s[2])
		acc[3] += int(s[3])
		acc[4] += int(s[4])
		acc[5] += int(s[5])
		acc[6] += int(s[6])
		acc[7] += int(s[7])
		acc[8] += int(s[8])
		acc[9] += int(s[9])
		acc[10] += int(s[10])
		acc[11] += int(s[11])
		acc[12] += int(s[12])
		acc[13] += int(s[13])
		acc[14] += int(s[14])
		acc[15] += int(s[15])
		si += ss
	}
	_ = out[15]
	for x := range 16 {
		out[x] = int16(acc[x] >> shift)
	}
}

func VectorVarianceScalar(ref, src []int16, n, shift int) int {
	if n <= 0 {
		return 0
	}
	ref = ref[:n]
	src = src[:n]
	var sum, sse0, sse1 int
	i := 0
	for ; i+3 < n; i += 4 {
		d0 := int(ref[i]) - int(src[i])
		d1 := int(ref[i+1]) - int(src[i+1])
		d2 := int(ref[i+2]) - int(src[i+2])
		d3 := int(ref[i+3]) - int(src[i+3])
		sum += d0 + d1 + d2 + d3
		sse0 += d0*d0 + d2*d2
		sse1 += d1*d1 + d3*d3
	}
	sse := sse0 + sse1
	for ; i < n; i++ {
		d := int(ref[i]) - int(src[i])
		sum += d
		sse += d * d
	}
	return sse - (sum * sum >> uint(shift))
}

func DenoiseFirstScalar(mc []byte, ms int, avg []byte, as int, sig []byte, ss, w, inc, level int) int {
	var columns [16]int
	mi, ai, si := 0, 0, 0
	t3 := 3 + inc
	l1 := level + 1
	l3 := level + 3
	for range w {
		mr := mc[mi : mi+w]
		ar := avg[ai : ai+w]
		sr := sig[si : si+w]
		for x := range w {
			sv := int(sr[x])
			d := int(mr[x]) - sv
			a := max(d, -d)
			adj := l3
			if a <= 15 {
				adj = l1
			}
			if a <= 7 {
				adj = level
			}
			if a <= t3 {
				adj = a
			}
			if d < 0 {
				adj = -adj
			}
			ar[x] = byte(min(max(sv+adj, 0), 255))
			columns[x] += adj
		}
		mi += ms
		ai += as
		si += ss
	}
	sum := 0
	for _, v := range columns[:w] {
		if w == 16 {
			v = min(v, 127)
		}
		sum += v
	}
	return sum
}

func Intra4Scalar(dst []byte, ds int, b *[16]byte, indices *[4][16]byte) {
	for i := range 16 {
		sum := 2 + int(b[indices[0][i]]) + int(b[indices[1][i]]) + int(b[indices[2][i]]) + int(b[indices[3][i]])
		dst[(i>>2)*ds+(i&3)] = byte(sum >> 2)
	}
}

func FillBlockScalar(dst []byte, ds, w, h int, v byte) {
	if w > 0 && h > 0 && ds >= 0 && len(dst) >= w && (h == 1 || ds <= (len(dst)-w)/(h-1)) {
		v64 := uint64(v) * 0x0101010101010101
		di := 0
		for range h {
			d := dst[di : di+w]
			x := 0
			for ; x+7 < w; x += 8 {
				binary.LittleEndian.PutUint64(d[x:], v64)
			}
			for ; x < w; x++ {
				d[x] = v
			}
			di += ds
		}
		return
	}
	for y := range h {
		for x := range w {
			dst[y*ds+x] = v
		}
	}
}

func RepeatRowScalar(dst []byte, ds int, a []byte, w int) {
	src := a[:w]
	di := 0
	for range w {
		copy(dst[di:di+w], src)
		di += ds
	}
}

func HorizontalScalar(dst []byte, ds int, l []byte, w int) {
	if w <= 0 {
		return
	}
	l = l[:w]
	di := 0
	for y := range w {
		v := l[y]
		v64 := uint64(v) * 0x0101010101010101
		d := dst[di : di+w]
		x := 0
		for ; x+7 < w; x += 8 {
			binary.LittleEndian.PutUint64(d[x:], v64)
		}
		for ; x < w; x++ {
			d[x] = v
		}
		di += ds
	}
}

func Mul16Scalar(a, b, out []int16, n int) {
	if n <= 0 {
		return
	}
	a = a[:n]
	b = b[:n]
	out = out[:n]
	i := 0
	for ; i+3 < n; i += 4 {
		out[i] = a[i] * b[i]
		out[i+1] = a[i+1] * b[i+1]
		out[i+2] = a[i+2] * b[i+2]
		out[i+3] = a[i+3] * b[i+3]
	}
	for ; i < n; i++ {
		out[i] = a[i] * b[i]
	}
}

func SAD16Scalar(src []byte, ss int, ref []byte, rs, h int) int {
	return sad16Scalar(src, ss, ref, rs, h)
}

func SAD16LimitScalar(src []byte, ss int, ref []byte, rs, h, limit int) (sum int) {
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
		s0 := max(d0, -d0) + max(d1, -d1) + max(d2, -d2) + max(d3, -d3)
		d4 := int(s[4]) - int(r[4])
		d5 := int(s[5]) - int(r[5])
		d6 := int(s[6]) - int(r[6])
		d7 := int(s[7]) - int(r[7])
		s1 := max(d4, -d4) + max(d5, -d5) + max(d6, -d6) + max(d7, -d7)
		d8 := int(s[8]) - int(r[8])
		d9 := int(s[9]) - int(r[9])
		d10 := int(s[10]) - int(r[10])
		d11 := int(s[11]) - int(r[11])
		s2 := max(d8, -d8) + max(d9, -d9) + max(d10, -d10) + max(d11, -d11)
		d12 := int(s[12]) - int(r[12])
		d13 := int(s[13]) - int(r[13])
		d14 := int(s[14]) - int(r[14])
		d15 := int(s[15]) - int(r[15])
		s3 := max(d12, -d12) + max(d13, -d13) + max(d14, -d14) + max(d15, -d15)
		sum += s0 + s1 + s2 + s3
		if sum > limit {
			return
		}
		si += ss
		ri += rs
	}
	return
}

func SAD16x4Scalar(src []byte, ss int, r0, r1, r2, r3 []byte, rs, h int, out *[4]uint32) {
	const mask00FF = uint64(0x00ff00ff00ff00ff)
	if h <= 128 {
		var a0Lo, a0Hi, a1Lo, a1Hi, a2Lo, a2Hi, a3Lo, a3Hi uint64
		si, ri := 0, 0
		for range h {
			s := src[si : si+16]
			p0 := r0[ri : ri+16]
			p1 := r1[ri : ri+16]
			p2 := r2[ri : ri+16]
			p3 := r3[ri : ri+16]
			sw0 := binary.LittleEndian.Uint64(s[0:8])
			sw0Lo, sw0Hi := sw0&mask00FF, (sw0>>8)&mask00FF
			b := binary.LittleEndian.Uint64(p0[0:8])
			a0Lo += sadSWAR4(sw0Lo, b&mask00FF)
			a0Hi += sadSWAR4(sw0Hi, (b>>8)&mask00FF)
			b = binary.LittleEndian.Uint64(p1[0:8])
			a1Lo += sadSWAR4(sw0Lo, b&mask00FF)
			a1Hi += sadSWAR4(sw0Hi, (b>>8)&mask00FF)
			b = binary.LittleEndian.Uint64(p2[0:8])
			a2Lo += sadSWAR4(sw0Lo, b&mask00FF)
			a2Hi += sadSWAR4(sw0Hi, (b>>8)&mask00FF)
			b = binary.LittleEndian.Uint64(p3[0:8])
			a3Lo += sadSWAR4(sw0Lo, b&mask00FF)
			a3Hi += sadSWAR4(sw0Hi, (b>>8)&mask00FF)

			sw1 := binary.LittleEndian.Uint64(s[8:16])
			sw1Lo, sw1Hi := sw1&mask00FF, (sw1>>8)&mask00FF
			b = binary.LittleEndian.Uint64(p0[8:16])
			a0Lo += sadSWAR4(sw1Lo, b&mask00FF)
			a0Hi += sadSWAR4(sw1Hi, (b>>8)&mask00FF)
			b = binary.LittleEndian.Uint64(p1[8:16])
			a1Lo += sadSWAR4(sw1Lo, b&mask00FF)
			a1Hi += sadSWAR4(sw1Hi, (b>>8)&mask00FF)
			b = binary.LittleEndian.Uint64(p2[8:16])
			a2Lo += sadSWAR4(sw1Lo, b&mask00FF)
			a2Hi += sadSWAR4(sw1Hi, (b>>8)&mask00FF)
			b = binary.LittleEndian.Uint64(p3[8:16])
			a3Lo += sadSWAR4(sw1Lo, b&mask00FF)
			a3Hi += sadSWAR4(sw1Hi, (b>>8)&mask00FF)

			si += ss
			ri += rs
		}
		out[0] = uint32(sumLanes16(a0Lo) + sumLanes16(a0Hi))
		out[1] = uint32(sumLanes16(a1Lo) + sumLanes16(a1Hi))
		out[2] = uint32(sumLanes16(a2Lo) + sumLanes16(a2Hi))
		out[3] = uint32(sumLanes16(a3Lo) + sumLanes16(a3Hi))
		return
	}
	out[0] = uint32(sad16Scalar(src, ss, r0, rs, h))
	out[1] = uint32(sad16Scalar(src, ss, r1, rs, h))
	out[2] = uint32(sad16Scalar(src, ss, r2, rs, h))
	out[3] = uint32(sad16Scalar(src, ss, r3, rs, h))
}

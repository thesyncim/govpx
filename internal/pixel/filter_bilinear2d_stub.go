//go:build !goexperiment.simd || !arm64 || purego

package pixel

import "encoding/binary"

func filterBilinear2DHalf16(src []byte, ss int, dst []byte, ds, h int) {
	const maskFE = uint64(0xfefefefefefefefe)
	s0 := src[:17]
	a0, b0 := binary.LittleEndian.Uint64(s0[0:8]), binary.LittleEndian.Uint64(s0[1:9])
	p0 := (a0 | b0) - (((a0 ^ b0) & maskFE) >> 1)
	a1, b1 := binary.LittleEndian.Uint64(s0[8:16]), binary.LittleEndian.Uint64(s0[9:17])
	p1 := (a1 | b1) - (((a1 ^ b1) & maskFE) >> 1)
	si, di := ss, 0
	for range h {
		s := src[si : si+17]
		d := dst[di : di+16]
		a0, b0 = binary.LittleEndian.Uint64(s[0:8]), binary.LittleEndian.Uint64(s[1:9])
		c0 := (a0 | b0) - (((a0 ^ b0) & maskFE) >> 1)
		binary.LittleEndian.PutUint64(d[0:8], (p0|c0)-(((p0^c0)&maskFE)>>1))
		p0 = c0

		a1, b1 = binary.LittleEndian.Uint64(s[8:16]), binary.LittleEndian.Uint64(s[9:17])
		c1 := (a1 | b1) - (((a1 ^ b1) & maskFE) >> 1)
		binary.LittleEndian.PutUint64(d[8:16], (p1|c1)-(((p1^c1)&maskFE)>>1))
		p1 = c1

		si += ss
		di += ds
	}
}

func filterBilinear2DHalf8(src []byte, ss int, dst []byte, ds, h int) {
	const maskFE = uint64(0xfefefefefefefefe)
	s0 := src[:9]
	a0, b0 := binary.LittleEndian.Uint64(s0[0:8]), binary.LittleEndian.Uint64(s0[1:9])
	p0 := (a0 | b0) - (((a0 ^ b0) & maskFE) >> 1)
	si, di := ss, 0
	for range h {
		s := src[si : si+9]
		a0, b0 = binary.LittleEndian.Uint64(s[0:8]), binary.LittleEndian.Uint64(s[1:9])
		c0 := (a0 | b0) - (((a0 ^ b0) & maskFE) >> 1)
		binary.LittleEndian.PutUint64(dst[di:di+8], (p0|c0)-(((p0^c0)&maskFE)>>1))
		p0 = c0
		si += ss
		di += ds
	}
}

func filterBilinear2DHalfWide(src []byte, ss int, dst []byte, ds, w, h int) {
	const maskFE = uint64(0xfefefefefefefefe)
	var prev [8]uint64
	cols := w >> 3
	s0 := src[:w+1]
	for c := range cols {
		x := c << 3
		a, b := binary.LittleEndian.Uint64(s0[x:x+8]), binary.LittleEndian.Uint64(s0[x+1:x+9])
		prev[c] = (a | b) - (((a ^ b) & maskFE) >> 1)
	}
	si, di := ss, 0
	for range h {
		s := src[si : si+w+1]
		d := dst[di : di+w]
		for c := range cols {
			x := c << 3
			a, b := binary.LittleEndian.Uint64(s[x:x+8]), binary.LittleEndian.Uint64(s[x+1:x+9])
			cur := (a | b) - (((a ^ b) & maskFE) >> 1)
			p := prev[c]
			prev[c] = cur
			binary.LittleEndian.PutUint64(d[x:x+8], (p|cur)-(((p^cur)&maskFE)>>1))
		}
		si += ss
		di += ds
	}
}

func filterBilinear2DGen16(src []byte, ss int, dst []byte, ds, h int, hf0, hf1, vf0, vf1 uint64) {
	const (
		mask00FF = uint64(0x00ff00ff00ff00ff)
		maskFF00 = uint64(0xff00ff00ff00ff00)
		bias64   = uint64(0x0040004000400040)
	)
	sFirst := src[:17]
	w0, w1 := binary.LittleEndian.Uint64(sFirst[0:8]), binary.LittleEndian.Uint64(sFirst[1:9])
	pLo0 := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
	pHi0 := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
	w2, w3 := binary.LittleEndian.Uint64(sFirst[8:16]), binary.LittleEndian.Uint64(sFirst[9:17])
	pLo1 := (((w2&mask00FF)*hf0 + (w3&mask00FF)*hf1 + bias64) >> 7) & mask00FF
	pHi1 := ((((w2>>8)&mask00FF)*hf0 + ((w3>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
	si, di := ss, 0
	for range h {
		s := src[si : si+17]
		d := dst[di : di+16]
		w0, w1 = binary.LittleEndian.Uint64(s[0:8]), binary.LittleEndian.Uint64(s[1:9])
		cLo0 := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		cHi0 := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		oLo0 := ((pLo0*vf0 + cLo0*vf1 + bias64) >> 7) & mask00FF
		oHi0 := ((pHi0*vf0 + cHi0*vf1 + bias64) << 1) & maskFF00
		binary.LittleEndian.PutUint64(d[0:8], oLo0|oHi0)
		pLo0, pHi0 = cLo0, cHi0

		w2, w3 = binary.LittleEndian.Uint64(s[8:16]), binary.LittleEndian.Uint64(s[9:17])
		cLo1 := (((w2&mask00FF)*hf0 + (w3&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		cHi1 := ((((w2>>8)&mask00FF)*hf0 + ((w3>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		oLo1 := ((pLo1*vf0 + cLo1*vf1 + bias64) >> 7) & mask00FF
		oHi1 := ((pHi1*vf0 + cHi1*vf1 + bias64) << 1) & maskFF00
		binary.LittleEndian.PutUint64(d[8:16], oLo1|oHi1)
		pLo1, pHi1 = cLo1, cHi1

		si += ss
		di += ds
	}
}

func filterBilinear2DGen8(src []byte, ss int, dst []byte, ds, h int, hf0, hf1, vf0, vf1 uint64) {
	const (
		mask00FF = uint64(0x00ff00ff00ff00ff)
		maskFF00 = uint64(0xff00ff00ff00ff00)
		bias64   = uint64(0x0040004000400040)
	)
	sFirst := src[:9]
	w0, w1 := binary.LittleEndian.Uint64(sFirst[0:8]), binary.LittleEndian.Uint64(sFirst[1:9])
	pLo := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
	pHi := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
	si, di := ss, 0
	for range h {
		s := src[si : si+9]
		w0, w1 = binary.LittleEndian.Uint64(s[0:8]), binary.LittleEndian.Uint64(s[1:9])
		cLo := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		cHi := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		oLo := ((pLo*vf0 + cLo*vf1 + bias64) >> 7) & mask00FF
		oHi := ((pHi*vf0 + cHi*vf1 + bias64) << 1) & maskFF00
		binary.LittleEndian.PutUint64(dst[di:di+8], oLo|oHi)
		pLo, pHi = cLo, cHi
		si += ss
		di += ds
	}
}

func filterBilinear2DGenWide(src []byte, ss int, dst []byte, ds, w, h int, hf0, hf1, vf0, vf1 uint64) {
	const (
		mask00FF = uint64(0x00ff00ff00ff00ff)
		maskFF00 = uint64(0xff00ff00ff00ff00)
		bias64   = uint64(0x0040004000400040)
	)
	var prevLo, prevHi [8]uint64
	cols := w >> 3
	sFirst := src[:w+1]
	for c := range cols {
		x := c << 3
		w0, w1 := binary.LittleEndian.Uint64(sFirst[x:x+8]), binary.LittleEndian.Uint64(sFirst[x+1:x+9])
		prevLo[c] = (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		prevHi[c] = ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
	}
	si, di := ss, 0
	for range h {
		s := src[si : si+w+1]
		d := dst[di : di+w]
		for c := range cols {
			x := c << 3
			w0, w1 := binary.LittleEndian.Uint64(s[x:x+8]), binary.LittleEndian.Uint64(s[x+1:x+9])
			cLo := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			cHi := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			oLo := ((prevLo[c]*vf0 + cLo*vf1 + bias64) >> 7) & mask00FF
			oHi := ((prevHi[c]*vf0 + cHi*vf1 + bias64) << 1) & maskFF00
			binary.LittleEndian.PutUint64(d[x:x+8], oLo|oHi)
			prevLo[c], prevHi[c] = cLo, cHi
		}
		si += ss
		di += ds
	}
}

func FilterBilinear2D(src []byte, ss int, dst []byte, ds, w, h int, hx0, hx1, hy0, hy1 uint8) bool {
	if w <= 0 || h <= 0 || ss < 0 || ds < 0 || w%8 != 0 || w > 64 {
		return false
	}
	if int(hx0)+int(hx1) != 128 || int(hy0)+int(hy1) != 128 {
		return false
	}
	if ss <= 1<<20 && ds <= 1<<20 && h <= 64 {
		if len(src) <= h*ss+w || len(dst) < (h-1)*ds+w {
			return false
		}
	} else if len(src) <= w || ss > (len(src)-1-w)/h || len(dst) < w || (h > 1 && ds > (len(dst)-w)/(h-1)) {
		return false
	}
	if &src[0] == &dst[0] {
		return false
	}
	_ = src[h*ss+w]
	_ = dst[(h-1)*ds+w-1]

	if hx0 == 64 && hx1 == 64 && hy0 == 64 && hy1 == 64 {
		switch w {
		case 16:
			filterBilinear2DHalf16(src, ss, dst, ds, h)
		case 8:
			filterBilinear2DHalf8(src, ss, dst, ds, h)
		default:
			filterBilinear2DHalfWide(src, ss, dst, ds, w, h)
		}
		return true
	}
	switch w {
	case 16:
		filterBilinear2DGen16(src, ss, dst, ds, h, uint64(hx0), uint64(hx1), uint64(hy0), uint64(hy1))
	case 8:
		filterBilinear2DGen8(src, ss, dst, ds, h, uint64(hx0), uint64(hx1), uint64(hy0), uint64(hy1))
	default:
		filterBilinear2DGenWide(src, ss, dst, ds, w, h, uint64(hx0), uint64(hx1), uint64(hy0), uint64(hy1))
	}
	return true
}

func FilterBilinear2DPair8x8(src0 []byte, ss0 int, src1 []byte, ss1 int, dst0 []byte, ds0 int, dst1 []byte, ds1 int, hx0, hx1, hy0, hy1 uint8) bool {
	if !FilterBilinear2D(src0, ss0, dst0, ds0, 8, 8, hx0, hx1, hy0, hy1) {
		return false
	}
	return FilterBilinear2D(src1, ss1, dst1, ds1, 8, 8, hx0, hx1, hy0, hy1)
}

func FilterHalfPelDisjoint(src []byte, ss int, dst []byte, ds, w, h, step int) {
	FilterBilinear(src, ss, dst, ds, w, h, step, 64, 64)
}

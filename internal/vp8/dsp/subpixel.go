package dsp

import (
	"encoding/binary"

	"github.com/thesyncim/govpx/internal/pixel"
	"github.com/thesyncim/govpx/internal/vp8/tables"
)

// Ported from libvpx v1.16.0 vp8/common/filter.c prediction paths.

func BilinearPredict4x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 4, 4)
}

func BilinearPredict8x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 4)
}

func BilinearPredict8x8(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if bilinearPredict8x8Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 8)
}

func BilinearPredict8x8Pair(
	src0 []byte, src0Stride int,
	src1 []byte, src1Stride int,
	xoffset int, yoffset int,
	dst0 []byte, dst0Stride int,
	dst1 []byte, dst1Stride int,
) {
	h := tables.BilinearFilters[xoffset]
	v := tables.BilinearFilters[yoffset]
	if pixel.FilterBilinear2DPair8x8(src0, src0Stride, src1, src1Stride, dst0, dst0Stride, dst1, dst1Stride,
		uint8(h[0]), uint8(h[1]), uint8(v[0]), uint8(v[1])) {
		return
	}
	BilinearPredict8x8(src0, src0Stride, xoffset, yoffset, dst0, dst0Stride)
	BilinearPredict8x8(src1, src1Stride, xoffset, yoffset, dst1, dst1Stride)
}

func BilinearPredict16x16(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if bilinearPredict16x16Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	bilinearPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 16)
}

// SixTapPredict4x4 expects src to start two rows and two columns before the
// prediction origin so the six filter taps are addressable with positive
// indexes.
func SixTapPredict4x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if xoffset|yoffset == 0 {
		copyCentralBlock(src, srcStride, dst, dstStride, 4, 4)
		return
	}
	if sixTapPredict4x4Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 4, 4)
}

func SixTapPredict16x8(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if xoffset != 0 || yoffset != 0 {
		if sixTapPredict16x8Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
			return
		}
		sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 8)
		return
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 16, 8)
}

func SixTapPredict8x16(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if xoffset != 0 || yoffset != 0 {
		if sixTapPredict8x16Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
			return
		}
		sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 16)
		return
	}
	copyCentralBlock(src, srcStride, dst, dstStride, 8, 16)
}

func SixTapPredict8x4(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if xoffset|yoffset == 0 {
		copyCentralBlock(src, srcStride, dst, dstStride, 8, 4)
		return
	}
	if sixTapPredict8x4Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 4)
}

func SixTapPredict8x8(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if xoffset|yoffset == 0 {
		copyCentralBlock(src, srcStride, dst, dstStride, 8, 8)
		return
	}
	if sixTapPredict8x8Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 8, 8)
}

func SixTapPredict8x8Pair(
	src0 []byte, src0Stride int,
	src1 []byte, src1Stride int,
	xoffset int, yoffset int,
	dst0 []byte, dst0Stride int,
	dst1 []byte, dst1Stride int,
) {
	if xoffset|yoffset == 0 {
		copyCentralBlock(src0, src0Stride, dst0, dst0Stride, 8, 8)
		copyCentralBlock(src1, src1Stride, dst1, dst1Stride, 8, 8)
		return
	}
	if sixTapPredict8x8PairMaybe(src0, src0Stride, src1, src1Stride, xoffset, yoffset, dst0, dst0Stride, dst1, dst1Stride) {
		return
	}
	SixTapPredict8x8(src0, src0Stride, xoffset, yoffset, dst0, dst0Stride)
	SixTapPredict8x8(src1, src1Stride, xoffset, yoffset, dst1, dst1Stride)
}

func SixTapPredict16x16(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int) {
	if xoffset|yoffset == 0 {
		copyCentralBlock(src, srcStride, dst, dstStride, 16, 16)
		return
	}
	if sixTapPredict16x16Maybe(src, srcStride, xoffset, yoffset, dst, dstStride) {
		return
	}
	sixTapPredict(src, srcStride, xoffset, yoffset, dst, dstStride, 16, 16)
}

func bilinearPredict(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, width int, height int) {
	if pixel.Enabled && srcStride >= width+1 && dstStride >= width && len(src) >= width+1 && srcStride <= (len(src)-width-1)/height && len(dst) >= width && dstStride <= (len(dst)-width)/(height-1) {
		bilinearPredictGo(src, srcStride, xoffset, yoffset, dst, dstStride, width, height)
		return
	}
	if xoffset == 0 && yoffset == 0 {
		pixel.CopyBlock(src, srcStride, dst, dstStride, width, height)
		return
	}
	const (
		maskFE   = uint64(0xfefefefefefefefe)
		mask00FF = uint64(0x00ff00ff00ff00ff)
		maskFF00 = uint64(0xff00ff00ff00ff00)
		bias64   = uint64(0x0040004000400040)
	)
	if yoffset == 0 {
		hFilter := tables.BilinearFilters[xoffset]
		h0, h1 := int(hFilter[0]), int(hFilter[1])
		si, di := 0, 0
		if width >= 8 {
			if h0 == 64 && h1 == 64 {
				for range height {
					s := src[si : si+width+1]
					d := dst[di : di+width]
					for x := 0; x < width; x += 8 {
						a := binary.LittleEndian.Uint64(s[x:])
						b := binary.LittleEndian.Uint64(s[x+1:])
						binary.LittleEndian.PutUint64(d[x:], (a|b)-(((a^b)&maskFE)>>1))
					}
					si += srcStride
					di += dstStride
				}
				return
			}
			f0, f1 := uint64(h0), uint64(h1)
			for range height {
				s := src[si : si+width+1]
				d := dst[di : di+width]
				for x := 0; x < width; x += 8 {
					w0 := binary.LittleEndian.Uint64(s[x:])
					w1 := binary.LittleEndian.Uint64(s[x+1:])
					lo := (((w0&mask00FF)*f0 + (w1&mask00FF)*f1 + bias64) >> 7) & mask00FF
					hi := ((((w0>>8)&mask00FF)*f0 + ((w1>>8)&mask00FF)*f1 + bias64) << 1) & maskFF00
					binary.LittleEndian.PutUint64(d[x:], lo|hi)
				}
				si += srcStride
				di += dstStride
			}
			return
		}
		for range height {
			s := src[si : si+width+1]
			d := dst[di : di+width]
			for x := 0; x < width; x += 4 {
				s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
				d[x] = uint8((s0*h0 + s1*h1 + 64) >> 7)
				d[x+1] = uint8((s1*h0 + s2*h1 + 64) >> 7)
				d[x+2] = uint8((s2*h0 + s3*h1 + 64) >> 7)
				d[x+3] = uint8((s3*h0 + s4*h1 + 64) >> 7)
			}
			si += srcStride
			di += dstStride
		}
		return
	}
	if xoffset == 0 {
		vFilter := tables.BilinearFilters[yoffset]
		v0, v1 := int(vFilter[0]), int(vFilter[1])
		si, di := 0, 0
		if width >= 8 {
			if v0 == 64 && v1 == 64 {
				for range height {
					s0 := src[si : si+width]
					s1 := src[si+srcStride : si+srcStride+width]
					d := dst[di : di+width]
					for x := 0; x < width; x += 8 {
						a := binary.LittleEndian.Uint64(s0[x:])
						b := binary.LittleEndian.Uint64(s1[x:])
						binary.LittleEndian.PutUint64(d[x:], (a|b)-(((a^b)&maskFE)>>1))
					}
					si += srcStride
					di += dstStride
				}
				return
			}
			f0, f1 := uint64(v0), uint64(v1)
			for range height {
				s0 := src[si : si+width]
				s1 := src[si+srcStride : si+srcStride+width]
				d := dst[di : di+width]
				for x := 0; x < width; x += 8 {
					w0 := binary.LittleEndian.Uint64(s0[x:])
					w1 := binary.LittleEndian.Uint64(s1[x:])
					lo := (((w0&mask00FF)*f0 + (w1&mask00FF)*f1 + bias64) >> 7) & mask00FF
					hi := ((((w0>>8)&mask00FF)*f0 + ((w1>>8)&mask00FF)*f1 + bias64) << 1) & maskFF00
					binary.LittleEndian.PutUint64(d[x:], lo|hi)
				}
				si += srcStride
				di += dstStride
			}
			return
		}
		for range height {
			s0 := src[si : si+width]
			s1 := src[si+srcStride : si+srcStride+width]
			d := dst[di : di+width]
			for x := 0; x < width; x += 4 {
				d[x] = uint8((int(s0[x])*v0 + int(s1[x])*v1 + 64) >> 7)
				d[x+1] = uint8((int(s0[x+1])*v0 + int(s1[x+1])*v1 + 64) >> 7)
				d[x+2] = uint8((int(s0[x+2])*v0 + int(s1[x+2])*v1 + 64) >> 7)
				d[x+3] = uint8((int(s0[x+3])*v0 + int(s1[x+3])*v1 + 64) >> 7)
			}
			si += srcStride
			di += dstStride
		}
		return
	}

	hFilter := tables.BilinearFilters[xoffset]
	vFilter := tables.BilinearFilters[yoffset]
	h0, h1 := int(hFilter[0]), int(hFilter[1])
	v0, v1 := int(vFilter[0]), int(vFilter[1])
	if width == 16 {
		hf0, hf1 := uint64(h0), uint64(h1)
		vf0, vf1 := uint64(v0), uint64(v1)
		sFirst := src[:17]
		w0 := binary.LittleEndian.Uint64(sFirst[0:])
		w1 := binary.LittleEndian.Uint64(sFirst[1:])
		pLo0 := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		pHi0 := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		w2 := binary.LittleEndian.Uint64(sFirst[8:])
		w3 := binary.LittleEndian.Uint64(sFirst[9:])
		pLo1 := (((w2&mask00FF)*hf0 + (w3&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		pHi1 := ((((w2>>8)&mask00FF)*hf0 + ((w3>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		si, di := srcStride, 0
		for range height {
			s := src[si : si+17]
			d := dst[di : di+16]
			w0 = binary.LittleEndian.Uint64(s[0:])
			w1 = binary.LittleEndian.Uint64(s[1:])
			cLo0 := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			cHi0 := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			oLo0 := ((pLo0*vf0 + cLo0*vf1 + bias64) >> 7) & mask00FF
			oHi0 := ((pHi0*vf0 + cHi0*vf1 + bias64) << 1) & maskFF00
			binary.LittleEndian.PutUint64(d[0:], oLo0|oHi0)
			pLo0, pHi0 = cLo0, cHi0

			w2 = binary.LittleEndian.Uint64(s[8:])
			w3 = binary.LittleEndian.Uint64(s[9:])
			cLo1 := (((w2&mask00FF)*hf0 + (w3&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			cHi1 := ((((w2>>8)&mask00FF)*hf0 + ((w3>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			oLo1 := ((pLo1*vf0 + cLo1*vf1 + bias64) >> 7) & mask00FF
			oHi1 := ((pHi1*vf0 + cHi1*vf1 + bias64) << 1) & maskFF00
			binary.LittleEndian.PutUint64(d[8:], oLo1|oHi1)
			pLo1, pHi1 = cLo1, cHi1

			si += srcStride
			di += dstStride
		}
		return
	}
	if width == 8 {
		hf0, hf1 := uint64(h0), uint64(h1)
		vf0, vf1 := uint64(v0), uint64(v1)
		sFirst := src[:9]
		w0 := binary.LittleEndian.Uint64(sFirst[0:])
		w1 := binary.LittleEndian.Uint64(sFirst[1:])
		pLo := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		pHi := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
		si, di := srcStride, 0
		for range height {
			s := src[si : si+9]
			d := dst[di : di+8]
			w0 = binary.LittleEndian.Uint64(s[0:])
			w1 = binary.LittleEndian.Uint64(s[1:])
			cLo := (((w0&mask00FF)*hf0 + (w1&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			cHi := ((((w0>>8)&mask00FF)*hf0 + ((w1>>8)&mask00FF)*hf1 + bias64) >> 7) & mask00FF
			oLo := ((pLo*vf0 + cLo*vf1 + bias64) >> 7) & mask00FF
			oHi := ((pHi*vf0 + cHi*vf1 + bias64) << 1) & maskFF00
			binary.LittleEndian.PutUint64(d[0:], oLo|oHi)
			pLo, pHi = cLo, cHi
			si += srcStride
			di += dstStride
		}
		return
	}
	var prev [16]uint16
	sFirst := src[:width+1]
	for x := range width {
		prev[x] = uint16((int(sFirst[x])*h0 + int(sFirst[x+1])*h1 + 64) >> 7)
	}
	si, di := srcStride, 0
	for range height {
		s := src[si : si+width+1]
		d := dst[di : di+width]
		for x := 0; x < width; x += 4 {
			s0, s1, s2, s3, s4 := int(s[x]), int(s[x+1]), int(s[x+2]), int(s[x+3]), int(s[x+4])
			b0 := (s0*h0 + s1*h1 + 64) >> 7
			b1 := (s1*h0 + s2*h1 + 64) >> 7
			b2 := (s2*h0 + s3*h1 + 64) >> 7
			b3 := (s3*h0 + s4*h1 + 64) >> 7
			a0, a1, a2, a3 := int(prev[x]), int(prev[x+1]), int(prev[x+2]), int(prev[x+3])
			prev[x], prev[x+1], prev[x+2], prev[x+3] = uint16(b0), uint16(b1), uint16(b2), uint16(b3)
			d[x] = uint8((a0*v0 + b0*v1 + 64) >> 7)
			d[x+1] = uint8((a1*v0 + b1*v1 + 64) >> 7)
			d[x+2] = uint8((a2*v0 + b2*v1 + 64) >> 7)
			d[x+3] = uint8((a3*v0 + b3*v1 + 64) >> 7)
		}
		si += srcStride
		di += dstStride
	}
}

func copyCentralBlock(src []byte, srcStride int, dst []byte, dstStride int, width int, height int) {
	srcOffset := 2*srcStride + 2
	for y := range height {
		copy(dst[y*dstStride:y*dstStride+width], src[srcOffset+y*srcStride:srcOffset+y*srcStride+width])
	}
}

func sixTapPredict(src []byte, srcStride int, xoffset int, yoffset int, dst []byte, dstStride int, width int, height int) {
	if yoffset == 0 {
		hFilter := tables.SubPelFilters[xoffset]
		pixel.Filter(src[2*srcStride:], srcStride, dst, dstStride, width, height, 1, hFilter[:])
		return
	}
	if xoffset == 0 {
		vFilter := tables.SubPelFilters[yoffset]
		pixel.Filter(src[2:], srcStride, dst, dstStride, width, height, srcStride, vFilter[:])
		return
	}
	hFilter := tables.SubPelFilters[xoffset]
	vFilter := tables.SubPelFilters[yoffset]
	var tmp [21 * 16]byte
	pixel.Filter(src, srcStride, tmp[:], width, width, height+5, 1, hFilter[:])
	pixel.Filter(tmp[:], width, dst, dstStride, width, height, width, vFilter[:])
}

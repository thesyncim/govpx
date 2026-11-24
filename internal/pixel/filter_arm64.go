//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// filterWide8 implements ARM64 NEON 8-tap and 2-tap subpixel convolution using
// unsigned widening multiplies (UMULL / MulWidenLo) and URHADD (Average).
//
// For all standard VP9 8-tap filters (regular, smooth, sharp, bilinear):
//   - Positive tap sum <= 192  => max positive product sum = 255*192 = 48960 < 65535 (fits uint16).
//   - Negative tap sum <= 102  => max negative product sum = 255*102 = 26010 < 65535 (fits uint16).
//   - (pos + 64) <= 49024 < 65535, so SubSaturated(neg) produces 0 whenever the
//     signed rounded result would clip to 0, and exact (pos - neg + 64) otherwise.
//   - Shifting right by 7 and SaturateToUint8 yields bit-exact clipPixel((sum+64)>>7).
func filterWide8(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) bool {
	if len(taps) != 8 || w <= 0 || w%8 != 0 || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	if step != 1 && step != ss {
		return false
	}
	_ = src[(h-1)*ss+w-1+7*step]
	_ = dst[(h-1)*ds+w-1]

	// Fast path: 2-tap bilinear filter embedded in 8-tap table ([0,0,0,t3,t4,0,0,0]).
	if (taps[0]|taps[1]|taps[2]|taps[5]|taps[6]|taps[7]) == 0 &&
		taps[3] >= 0 && taps[4] >= 0 && int(taps[3])+int(taps[4]) <= 128 {
		return filterBilinear8ARM64(src[3*step:], ss, dst, ds, w, h, step, uint8(taps[3]), uint8(taps[4]))
	}

	mode := narrow8Mode(taps)
	if mode == 0 {
		return false
	}

	// Verify unsigned positive/negative sums fit uint16 with +64 bias.
	var posSum, negSum int
	for _, t := range taps {
		if t >= 0 {
			posSum += int(t)
		} else {
			negSum -= int(t)
		}
	}
	if posSum*255+64 > 65535 || negSum*255 > 65535 {
		return false
	}

	if step == ss {
		return filterVert8ARM64(src, ss, dst, ds, w, h, taps, mode)
	}
	return filterHoriz8ARM64(src, ss, dst, ds, w, h, taps, mode)
}

func filterLinearFast(src []byte, ss int, dst []byte, ds, w, h, step int, t0, t1 uint8) bool {
	return filterBilinear8ARM64(src, ss, dst, ds, w, h, step, t0, t1)
}

func filterBilinear8ARM64(src []byte, ss int, dst []byte, ds, w, h, step int, t0, t1 uint8) bool {
	if w <= 0 || h <= 0 || ss < 0 || ds < 0 || step < 0 {
		return false
	}
	_ = src[(h-1)*ss+w-1+step]
	_ = dst[(h-1)*ds+w-1]
	// Half-pel (64, 64): single NEON URHADD instruction per 16 pixels.
	if t0 == 64 && t1 == 64 {
		if step == 1 {
			if w == 4 {
				so, do := 0, 0
				y := 0
				for ; y+3 < h; y += 4 {
					s0, s1, s2, s3 := src[so:], src[so+ss:], src[so+2*ss:], src[so+3*ss:]
					a := archsimd.LoadUint32x4Array(&[4]uint32{
						binary.LittleEndian.Uint32(s0),
						binary.LittleEndian.Uint32(s1),
						binary.LittleEndian.Uint32(s2),
						binary.LittleEndian.Uint32(s3),
					}).ReshapeToUint8s()
					b := archsimd.LoadUint32x4Array(&[4]uint32{
						binary.LittleEndian.Uint32(s0[1:]),
						binary.LittleEndian.Uint32(s1[1:]),
						binary.LittleEndian.Uint32(s2[1:]),
						binary.LittleEndian.Uint32(s3[1:]),
					}).ReshapeToUint8s()
					out := a.Average(b).ReshapeToUint32s()
					binary.LittleEndian.PutUint32(dst[do:], out.GetElem(0))
					binary.LittleEndian.PutUint32(dst[do+ds:], out.GetElem(1))
					binary.LittleEndian.PutUint32(dst[do+2*ds:], out.GetElem(2))
					binary.LittleEndian.PutUint32(dst[do+3*ds:], out.GetElem(3))
					so += 4 * ss
					do += 4 * ds
				}
				for ; y < h; y++ {
					a := archsimd.LoadUint32x4Array(&[4]uint32{binary.LittleEndian.Uint32(src[so:])}).ReshapeToUint8s()
					b := archsimd.LoadUint32x4Array(&[4]uint32{binary.LittleEndian.Uint32(src[so+1:])}).ReshapeToUint8s()
					binary.LittleEndian.PutUint32(dst[do:], a.Average(b).ReshapeToUint32s().GetElem(0))
					so += ss
					do += ds
				}
				return true
			}
			if w == 8 {
				so, do := 0, 0
				y := 0
				for ; y+1 < h; y += 2 {
					s0, s1 := src[so:], src[so+ss:]
					a := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(s0), binary.LittleEndian.Uint64(s1)}).ReshapeToUint8s()
					b := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(s0[1:]), binary.LittleEndian.Uint64(s1[1:])}).ReshapeToUint8s()
					out := a.Average(b).ReshapeToUint64s()
					binary.LittleEndian.PutUint64(dst[do:], out.GetElem(0))
					binary.LittleEndian.PutUint64(dst[do+ds:], out.GetElem(1))
					so += 2 * ss
					do += 2 * ds
				}
				if y < h {
					a, b := load8(src[so:]), load8(src[so+1:])
					storeBytes8(dst[do:], a.Average(b))
				}
				return true
			}
			for y := range h {
				so, do := y*ss, y*ds
				x := 0
				for ; x+16 <= w; x += 16 {
					row := src[so+x : so+x+17]
					archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))).StoreArray((*[16]byte)(dst[do+x : do+x+16]))
				}
				if x < w {
					a, b := load8(src[so+x:]), load8(src[so+x+1:])
					storeBytes8(dst[do+x:], a.Average(b))
				}
			}
			return true
		}
		// Vertical half-pel.
		if w == 4 {
			r0 := binary.LittleEndian.Uint32(src)
			so, do := ss, 0
			y := 0
			for ; y+3 < h; y += 4 {
				r1 := binary.LittleEndian.Uint32(src[so:])
				r2 := binary.LittleEndian.Uint32(src[so+ss:])
				r3 := binary.LittleEndian.Uint32(src[so+2*ss:])
				r4 := binary.LittleEndian.Uint32(src[so+3*ss:])
				a := archsimd.LoadUint32x4Array(&[4]uint32{r0, r1, r2, r3}).ReshapeToUint8s()
				b := archsimd.LoadUint32x4Array(&[4]uint32{r1, r2, r3, r4}).ReshapeToUint8s()
				out := a.Average(b).ReshapeToUint32s()
				binary.LittleEndian.PutUint32(dst[do:], out.GetElem(0))
				binary.LittleEndian.PutUint32(dst[do+ds:], out.GetElem(1))
				binary.LittleEndian.PutUint32(dst[do+2*ds:], out.GetElem(2))
				binary.LittleEndian.PutUint32(dst[do+3*ds:], out.GetElem(3))
				r0 = r4
				so += 4 * ss
				do += 4 * ds
			}
			for ; y < h; y++ {
				r1 := binary.LittleEndian.Uint32(src[so:])
				a := archsimd.LoadUint32x4Array(&[4]uint32{r0}).ReshapeToUint8s()
				b := archsimd.LoadUint32x4Array(&[4]uint32{r1}).ReshapeToUint8s()
				binary.LittleEndian.PutUint32(dst[do:], a.Average(b).ReshapeToUint32s().GetElem(0))
				r0 = r1
				so += ss
				do += ds
			}
			return true
		}
		if w == 8 {
			r0 := binary.LittleEndian.Uint64(src)
			so, do := ss, 0
			y := 0
			for ; y+1 < h; y += 2 {
				r1 := binary.LittleEndian.Uint64(src[so:])
				r2 := binary.LittleEndian.Uint64(src[so+ss:])
				a := archsimd.LoadUint64x2Array(&[2]uint64{r0, r1}).ReshapeToUint8s()
				b := archsimd.LoadUint64x2Array(&[2]uint64{r1, r2}).ReshapeToUint8s()
				out := a.Average(b).ReshapeToUint64s()
				binary.LittleEndian.PutUint64(dst[do:], out.GetElem(0))
				binary.LittleEndian.PutUint64(dst[do+ds:], out.GetElem(1))
				r0 = r2
				so += 2 * ss
				do += 2 * ds
			}
			if y < h {
				a, b := load8(src[so-ss:]), load8(src[so:])
				storeBytes8(dst[do:], a.Average(b))
			}
			return true
		}
		for x := 0; x < w; {
			if x+16 <= w {
				a := archsimd.LoadUint8x16Array((*[16]byte)(src[x : x+16]))
				so, do := ss+x, x
				for range h {
					b := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
					a.Average(b).StoreArray((*[16]byte)(dst[do : do+16]))
					a = b
					so += ss
					do += ds
				}
				x += 16
			} else {
				a := load8(src[x:])
				so, do := ss+x, x
				for range h {
					b := load8(src[so:])
					storeBytes8(dst[do:], a.Average(b))
					a = b
					so += ss
					do += ds
				}
				x += 8
			}
		}
		return true
	}

	// General 2-tap non-negative bilinear using UMULL (MulWidenLo).
	c0, c1 := archsimd.BroadcastUint8x16(t0), archsimd.BroadcastUint8x16(t1)
	bias := archsimd.BroadcastUint16x8(64)
	if step == 1 {
		if w == 4 {
			so, do := 0, 0
			y := 0
			for ; y+3 < h; y += 4 {
				s0, s1, s2, s3 := src[so:], src[so+ss:], src[so+2*ss:], src[so+3*ss:]
				a := archsimd.LoadUint32x4Array(&[4]uint32{
					binary.LittleEndian.Uint32(s0),
					binary.LittleEndian.Uint32(s1),
					binary.LittleEndian.Uint32(s2),
					binary.LittleEndian.Uint32(s3),
				}).ReshapeToUint8s()
				b := archsimd.LoadUint32x4Array(&[4]uint32{
					binary.LittleEndian.Uint32(s0[1:]),
					binary.LittleEndian.Uint32(s1[1:]),
					binary.LittleEndian.Uint32(s2[1:]),
					binary.LittleEndian.Uint32(s3[1:]),
				}).ReshapeToUint8s()
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				hi := a.HiToLo().MulWidenLo(c0).Add(b.HiToLo().MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				out := lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()).ReshapeToUint32s()
				binary.LittleEndian.PutUint32(dst[do:], out.GetElem(0))
				binary.LittleEndian.PutUint32(dst[do+ds:], out.GetElem(1))
				binary.LittleEndian.PutUint32(dst[do+2*ds:], out.GetElem(2))
				binary.LittleEndian.PutUint32(dst[do+3*ds:], out.GetElem(3))
				so += 4 * ss
				do += 4 * ds
			}
			for ; y < h; y++ {
				a := archsimd.LoadUint32x4Array(&[4]uint32{binary.LittleEndian.Uint32(src[so:])}).ReshapeToUint8s()
				b := archsimd.LoadUint32x4Array(&[4]uint32{binary.LittleEndian.Uint32(src[so+1:])}).ReshapeToUint8s()
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				binary.LittleEndian.PutUint32(dst[do:], lo.TruncToUint8().ReshapeToUint32s().GetElem(0))
				so += ss
				do += ds
			}
			return true
		}
		if w == 8 {
			so, do := 0, 0
			y := 0
			for ; y+1 < h; y += 2 {
				s0, s1 := src[so:], src[so+ss:]
				a := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(s0), binary.LittleEndian.Uint64(s1)}).ReshapeToUint8s()
				b := archsimd.LoadUint64x2Array(&[2]uint64{binary.LittleEndian.Uint64(s0[1:]), binary.LittleEndian.Uint64(s1[1:])}).ReshapeToUint8s()
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				hi := a.HiToLo().MulWidenLo(c0).Add(b.HiToLo().MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				out := lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()).ReshapeToUint64s()
				binary.LittleEndian.PutUint64(dst[do:], out.GetElem(0))
				binary.LittleEndian.PutUint64(dst[do+ds:], out.GetElem(1))
				so += 2 * ss
				do += 2 * ds
			}
			if y < h {
				a, b := load8(src[so:]), load8(src[so+1:])
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				storeBytes8(dst[do:], lo.TruncToUint8())
			}
			return true
		}
		for y := range h {
			so, do := y*ss, y*ds
			x := 0
			for ; x+16 <= w; x += 16 {
				row := src[so+x : so+x+17]
				a, b := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				hi := a.HiToLo().MulWidenLo(c0).Add(b.HiToLo().MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()).StoreArray((*[16]byte)(dst[do+x : do+x+16]))
			}
			if x < w {
				a, b := load8(src[so+x:]), load8(src[so+x+1:])
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				storeBytes8(dst[do+x:], lo.TruncToUint8())
			}
		}
		return true
	}
	if w == 4 {
		r0 := binary.LittleEndian.Uint32(src)
		so, do := ss, 0
		y := 0
		for ; y+3 < h; y += 4 {
			r1 := binary.LittleEndian.Uint32(src[so:])
			r2 := binary.LittleEndian.Uint32(src[so+ss:])
			r3 := binary.LittleEndian.Uint32(src[so+2*ss:])
			r4 := binary.LittleEndian.Uint32(src[so+3*ss:])
			a := archsimd.LoadUint32x4Array(&[4]uint32{r0, r1, r2, r3}).ReshapeToUint8s()
			b := archsimd.LoadUint32x4Array(&[4]uint32{r1, r2, r3, r4}).ReshapeToUint8s()
			lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
			hi := a.HiToLo().MulWidenLo(c0).Add(b.HiToLo().MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
			out := lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()).ReshapeToUint32s()
			binary.LittleEndian.PutUint32(dst[do:], out.GetElem(0))
			binary.LittleEndian.PutUint32(dst[do+ds:], out.GetElem(1))
			binary.LittleEndian.PutUint32(dst[do+2*ds:], out.GetElem(2))
			binary.LittleEndian.PutUint32(dst[do+3*ds:], out.GetElem(3))
			r0 = r4
			so += 4 * ss
			do += 4 * ds
		}
		for ; y < h; y++ {
			r1 := binary.LittleEndian.Uint32(src[so:])
			a := archsimd.LoadUint32x4Array(&[4]uint32{r0}).ReshapeToUint8s()
			b := archsimd.LoadUint32x4Array(&[4]uint32{r1}).ReshapeToUint8s()
			lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
			binary.LittleEndian.PutUint32(dst[do:], lo.TruncToUint8().ReshapeToUint32s().GetElem(0))
			r0 = r1
			so += ss
			do += ds
		}
		return true
	}
	if w == 8 {
		r0 := binary.LittleEndian.Uint64(src)
		so, do := ss, 0
		y := 0
		for ; y+1 < h; y += 2 {
			r1 := binary.LittleEndian.Uint64(src[so:])
			r2 := binary.LittleEndian.Uint64(src[so+ss:])
			a := archsimd.LoadUint64x2Array(&[2]uint64{r0, r1}).ReshapeToUint8s()
			b := archsimd.LoadUint64x2Array(&[2]uint64{r1, r2}).ReshapeToUint8s()
			lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
			hi := a.HiToLo().MulWidenLo(c0).Add(b.HiToLo().MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
			out := lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()).ReshapeToUint64s()
			binary.LittleEndian.PutUint64(dst[do:], out.GetElem(0))
			binary.LittleEndian.PutUint64(dst[do+ds:], out.GetElem(1))
			r0 = r2
			so += 2 * ss
			do += 2 * ds
		}
		if y < h {
			a, b := load8(src[so-ss:]), load8(src[so:])
			lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
			storeBytes8(dst[do:], lo.TruncToUint8())
		}
		return true
	}
	for x := 0; x < w; {
		if x+16 <= w {
			a := archsimd.LoadUint8x16Array((*[16]byte)(src[x : x+16]))
			so, do := ss+x, x
			for range h {
				b := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				hi := a.HiToLo().MulWidenLo(c0).Add(b.HiToLo().MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()).StoreArray((*[16]byte)(dst[do : do+16]))
				a = b
				so += ss
				do += ds
			}
			x += 16
		} else {
			a := load8(src[x:])
			so, do := ss+x, x
			for range h {
				b := load8(src[so:])
				lo := a.MulWidenLo(c0).Add(b.MulWidenLo(c1)).Add(bias).ShiftAllRight(7)
				storeBytes8(dst[do:], lo.TruncToUint8())
				a = b
				so += ss
				do += ds
			}
			x += 8
		}
	}
	return true
}

func filterAbsU8(t int16) archsimd.Uint8x16 {
	if t < 0 {
		t = -t
	}
	return archsimd.BroadcastUint8x16(uint8(t))
}

// filterHoriz8ARM64 runs 8-tap horizontal convolution via uint8 MulWidenLo.
// For 16-wide chunks, two overlapping 16-byte loads feed ConcatShiftBytesRight
// to produce all 8 shifted tap vectors for 16 output pixels with zero widen8s.
func filterHoriz8ARM64(src []byte, ss int, dst []byte, ds, w, h int, taps []int16, mode int) bool {
	c0, c1, c2, c3 := filterAbsU8(taps[0]), filterAbsU8(taps[1]), filterAbsU8(taps[2]), filterAbsU8(taps[3])
	c4, c5, c6, c7 := filterAbsU8(taps[4]), filterAbsU8(taps[5]), filterAbsU8(taps[6]), filterAbsU8(taps[7])
	bias := archsimd.BroadcastUint16x8(64)

	for y := range h {
		so, do := y*ss, y*ds
		x := 0
		for ; x+16 <= w; x += 16 {
			row := src[so+x : so+x+23]
			v0 := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16]))
			v1 := archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
			v2 := archsimd.LoadUint8x16Array((*[16]byte)(row[2:18]))
			v3 := archsimd.LoadUint8x16Array((*[16]byte)(row[3:19]))
			v4 := archsimd.LoadUint8x16Array((*[16]byte)(row[4:20]))
			v5 := archsimd.LoadUint8x16Array((*[16]byte)(row[5:21]))
			v6 := archsimd.LoadUint8x16Array((*[16]byte)(row[6:22]))
			v7 := archsimd.LoadUint8x16Array((*[16]byte)(row[7:23]))

			var posLo, negLo, posHi, negHi archsimd.Uint16x8
			if mode == 1 {
				negLo = v0.MulWidenLo(c0).Add(v2.MulWidenLo(c2)).Add(v5.MulWidenLo(c5)).Add(v7.MulWidenLo(c7))
				posLo = bias.Add(v1.MulWidenLo(c1)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v6.MulWidenLo(c6))
				negHi = v0.HiToLo().MulWidenLo(c0).Add(v2.HiToLo().MulWidenLo(c2)).Add(v5.HiToLo().MulWidenLo(c5)).Add(v7.HiToLo().MulWidenLo(c7))
				posHi = bias.Add(v1.HiToLo().MulWidenLo(c1)).Add(v3.HiToLo().MulWidenLo(c3)).Add(v4.HiToLo().MulWidenLo(c4)).Add(v6.HiToLo().MulWidenLo(c6))
			} else {
				negLo = v0.MulWidenLo(c0).Add(v1.MulWidenLo(c1)).Add(v6.MulWidenLo(c6)).Add(v7.MulWidenLo(c7))
				posLo = bias.Add(v2.MulWidenLo(c2)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v5.MulWidenLo(c5))
				negHi = v0.HiToLo().MulWidenLo(c0).Add(v1.HiToLo().MulWidenLo(c1)).Add(v6.HiToLo().MulWidenLo(c6)).Add(v7.HiToLo().MulWidenLo(c7))
				posHi = bias.Add(v2.HiToLo().MulWidenLo(c2)).Add(v3.HiToLo().MulWidenLo(c3)).Add(v4.HiToLo().MulWidenLo(c4)).Add(v5.HiToLo().MulWidenLo(c5))
			}
			outLo := posLo.SubSaturated(negLo).ShiftAllRight(7).SaturateToUint8()
			outHi := posHi.SubSaturated(negHi).ShiftAllRight(7).SaturateToUint8()
			outLo.ReshapeToUint64s().InterleaveLo(outHi.ReshapeToUint64s()).ReshapeToUint8s().Store(dst[do+x : do+x+16])
		}
		if x < w {
			p := src[so+x:]
			var v0, v1, v2, v3, v4, v5, v6, v7 archsimd.Uint8x16
			if len(p) >= 16 {
				v0 = archsimd.LoadUint8x16(p[:16])
				z := archsimd.Uint8x16{}
				v1 = z.ConcatShiftBytesRight(v0, 1)
				v2 = z.ConcatShiftBytesRight(v0, 2)
				v3 = z.ConcatShiftBytesRight(v0, 3)
				v4 = z.ConcatShiftBytesRight(v0, 4)
				v5 = z.ConcatShiftBytesRight(v0, 5)
				v6 = z.ConcatShiftBytesRight(v0, 6)
				v7 = z.ConcatShiftBytesRight(v0, 7)
			} else {
				v0, v1, v2, v3 = load8(p), load8(p[1:]), load8(p[2:]), load8(p[3:])
				v4, v5, v6, v7 = load8(p[4:]), load8(p[5:]), load8(p[6:]), load8(p[7:])
			}
			var posLo, negLo archsimd.Uint16x8
			if mode == 1 {
				negLo = v0.MulWidenLo(c0).Add(v2.MulWidenLo(c2)).Add(v5.MulWidenLo(c5)).Add(v7.MulWidenLo(c7))
				posLo = bias.Add(v1.MulWidenLo(c1)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v6.MulWidenLo(c6))
			} else {
				negLo = v0.MulWidenLo(c0).Add(v1.MulWidenLo(c1)).Add(v6.MulWidenLo(c6)).Add(v7.MulWidenLo(c7))
				posLo = bias.Add(v2.MulWidenLo(c2)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v5.MulWidenLo(c5))
			}
			storeBytes8(dst[do+x:], posLo.SubSaturated(negLo).ShiftAllRight(7).SaturateToUint8())
		}
	}
	return true
}

// filterVert8ARM64 runs 8-tap vertical convolution via uint8 MulWidenLo.
// Keeping 7 Uint8x16 sliding rows in registers processes 16 columns at a time
// with one 16-byte load and one 16-byte store per output row.
func filterVert8ARM64(src []byte, ss int, dst []byte, ds, w, h int, taps []int16, mode int) bool {
	c0, c1, c2, c3 := filterAbsU8(taps[0]), filterAbsU8(taps[1]), filterAbsU8(taps[2]), filterAbsU8(taps[3])
	c4, c5, c6, c7 := filterAbsU8(taps[4]), filterAbsU8(taps[5]), filterAbsU8(taps[6]), filterAbsU8(taps[7])
	bias := archsimd.BroadcastUint16x8(64)

	x := 0
	for ; x+16 <= w; x += 16 {
		v0 := archsimd.LoadUint8x16Array((*[16]byte)(src[0*ss+x : 0*ss+x+16]))
		v1 := archsimd.LoadUint8x16Array((*[16]byte)(src[1*ss+x : 1*ss+x+16]))
		v2 := archsimd.LoadUint8x16Array((*[16]byte)(src[2*ss+x : 2*ss+x+16]))
		v3 := archsimd.LoadUint8x16Array((*[16]byte)(src[3*ss+x : 3*ss+x+16]))
		v4 := archsimd.LoadUint8x16Array((*[16]byte)(src[4*ss+x : 4*ss+x+16]))
		v5 := archsimd.LoadUint8x16Array((*[16]byte)(src[5*ss+x : 5*ss+x+16]))
		v6 := archsimd.LoadUint8x16Array((*[16]byte)(src[6*ss+x : 6*ss+x+16]))
		h0, h1, h2, h3 := v0.HiToLo(), v1.HiToLo(), v2.HiToLo(), v3.HiToLo()
		h4, h5, h6 := v4.HiToLo(), v5.HiToLo(), v6.HiToLo()
		so, do := 7*ss+x, x
		if mode == 1 {
			for range h {
				v7 := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				h7 := v7.HiToLo()
				negLo := v0.MulWidenLo(c0).Add(v2.MulWidenLo(c2)).Add(v5.MulWidenLo(c5)).Add(v7.MulWidenLo(c7))
				posLo := bias.Add(v1.MulWidenLo(c1)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v6.MulWidenLo(c6))
				negHi := h0.MulWidenLo(c0).Add(h2.MulWidenLo(c2)).Add(h5.MulWidenLo(c5)).Add(h7.MulWidenLo(c7))
				posHi := bias.Add(h1.MulWidenLo(c1)).Add(h3.MulWidenLo(c3)).Add(h4.MulWidenLo(c4)).Add(h6.MulWidenLo(c6))
				outLo := posLo.SubSaturated(negLo).ShiftAllRight(7).SaturateToUint8()
				outHi := posHi.SubSaturated(negHi).ShiftAllRight(7).SaturateToUint8()
				outLo.ReshapeToUint64s().InterleaveLo(outHi.ReshapeToUint64s()).ReshapeToUint8s().StoreArray((*[16]byte)(dst[do : do+16]))
				v0, v1, v2, v3, v4, v5, v6 = v1, v2, v3, v4, v5, v6, v7
				h0, h1, h2, h3, h4, h5, h6 = h1, h2, h3, h4, h5, h6, h7
				so += ss
				do += ds
			}
		} else {
			for range h {
				v7 := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				h7 := v7.HiToLo()
				negLo := v0.MulWidenLo(c0).Add(v1.MulWidenLo(c1)).Add(v6.MulWidenLo(c6)).Add(v7.MulWidenLo(c7))
				posLo := bias.Add(v2.MulWidenLo(c2)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v5.MulWidenLo(c5))
				negHi := h0.MulWidenLo(c0).Add(h1.MulWidenLo(c1)).Add(h6.MulWidenLo(c6)).Add(h7.MulWidenLo(c7))
				posHi := bias.Add(h2.MulWidenLo(c2)).Add(h3.MulWidenLo(c3)).Add(h4.MulWidenLo(c4)).Add(h5.MulWidenLo(c5))
				outLo := posLo.SubSaturated(negLo).ShiftAllRight(7).SaturateToUint8()
				outHi := posHi.SubSaturated(negHi).ShiftAllRight(7).SaturateToUint8()
				outLo.ReshapeToUint64s().InterleaveLo(outHi.ReshapeToUint64s()).ReshapeToUint8s().StoreArray((*[16]byte)(dst[do : do+16]))
				v0, v1, v2, v3, v4, v5, v6 = v1, v2, v3, v4, v5, v6, v7
				h0, h1, h2, h3, h4, h5, h6 = h1, h2, h3, h4, h5, h6, h7
				so += ss
				do += ds
			}
		}
	}
	if x < w {
		v0 := load8(src[0*ss+x:])
		v1 := load8(src[1*ss+x:])
		v2 := load8(src[2*ss+x:])
		v3 := load8(src[3*ss+x:])
		v4 := load8(src[4*ss+x:])
		v5 := load8(src[5*ss+x:])
		v6 := load8(src[6*ss+x:])
		so, do := 7*ss+x, x
		for range h {
			v7 := load8(src[so:])
			var posLo, negLo archsimd.Uint16x8
			if mode == 1 {
				negLo = v0.MulWidenLo(c0).Add(v2.MulWidenLo(c2)).Add(v5.MulWidenLo(c5)).Add(v7.MulWidenLo(c7))
				posLo = bias.Add(v1.MulWidenLo(c1)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v6.MulWidenLo(c6))
			} else {
				negLo = v0.MulWidenLo(c0).Add(v1.MulWidenLo(c1)).Add(v6.MulWidenLo(c6)).Add(v7.MulWidenLo(c7))
				posLo = bias.Add(v2.MulWidenLo(c2)).Add(v3.MulWidenLo(c3)).Add(v4.MulWidenLo(c4)).Add(v5.MulWidenLo(c5))
			}
			storeBytes8(dst[do:], posLo.SubSaturated(negLo).ShiftAllRight(7).SaturateToUint8())
			v0, v1, v2, v3, v4, v5, v6 = v1, v2, v3, v4, v5, v6, v7
			so += ss
			do += ds
		}
	}
	return true
}

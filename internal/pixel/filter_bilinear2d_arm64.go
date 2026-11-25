//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// FilterBilinear2D performs fused 1-pass 2D bilinear interpolation directly
// from src into dst without an intermediate scratch buffer, bit-exact with
// sequential horizontal (hx0, hx1) followed by vertical (hy0, hy1) passes.
func FilterBilinear2D(src []byte, ss int, dst []byte, ds, w, h int, hx0, hx1, hy0, hy1 uint8) bool {
	if !Enabled || w <= 0 || h <= 0 || ss < 0 || ds < 0 || (w != 4 && w%8 != 0) {
		return false
	}
	if int(hx0)+int(hx1) > 128 || int(hy0)+int(hy1) > 128 {
		return false
	}
	if ss <= 1<<20 && ds <= 1<<20 && w <= 64 && h <= 64 {
		if len(src) <= h*ss+w || len(dst) < (h-1)*ds+w {
			return false
		}
	} else if len(src) <= w || ss > (len(src)-1-w)/h || len(dst) < w || (h > 1 && ds > (len(dst)-w)/(h-1)) {
		return false
	}
	// Disjointness guard: fused rolling rows require dst not to overwrite
	// unread rows of src.
	if slicesOverlap(src, dst, h*ss+w+1, (h-1)*ds+w) {
		return false
	}
	_ = src[h*ss+w]
	_ = dst[(h-1)*ds+w-1]

	// Fast path 1: 2D half-pel (64,64) x (64,64) — 2 NEON URHADD per 16 pixels.
	if hx0 == 64 && hx1 == 64 && hy0 == 64 && hy1 == 64 {
		if w == 16 {
			r0 := src[0:17]
			prev := archsimd.LoadUint8x16Array((*[16]byte)(r0[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(r0[1:17])))
			so, do := ss, 0
			for range h {
				row := src[so : so+17]
				cur := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row[1:17])))
				prev.Average(cur).StoreArray((*[16]byte)(dst[do : do+16]))
				prev = cur
				so += ss
				do += ds
			}
			return true
		}
		if w == 8 {
			prev := load8(src[0:]).Average(load8(src[1:]))
			so, do := ss, 0
			for range h {
				cur := load8(src[so:]).Average(load8(src[so+1:]))
				storeBytes8(dst[do:], prev.Average(cur))
				prev = cur
				so += ss
				do += ds
			}
			return true
		}
		if w == 4 {
			prev := load4(src[0:]).Average(load4(src[1:]))
			so, do := ss, 0
			for range h {
				cur := load4(src[so:]).Average(load4(src[so+1:]))
				binary.LittleEndian.PutUint32(dst[do:], prev.Average(cur).ReshapeToUint32s().GetElem(0))
				prev = cur
				so += ss
				do += ds
			}
			return true
		}
		for x := 0; x < w; {
			if x+16 <= w {
				r0 := src[x : x+17]
				prev := archsimd.LoadUint8x16Array((*[16]byte)(r0[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(r0[1:17])))
				so, do := ss+x, x
				for range h {
					row := src[so : so+17]
					cur := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row[1:17])))
					prev.Average(cur).StoreArray((*[16]byte)(dst[do : do+16]))
					prev = cur
					so += ss
					do += ds
				}
				x += 16
			} else {
				prev := load8(src[x:]).Average(load8(src[x+1:]))
				so, do := ss+x, x
				for range h {
					cur := load8(src[so:]).Average(load8(src[so+1:]))
					storeBytes8(dst[do:], prev.Average(cur))
					prev = cur
					so += ss
					do += ds
				}
				x += 8
			}
		}
		return true
	}

	cx0, cx1 := archsimd.BroadcastUint8x16(hx0), archsimd.BroadcastUint8x16(hx1)
	cy0, cy1 := archsimd.BroadcastUint8x16(hy0), archsimd.BroadcastUint8x16(hy1)
	bias := archsimd.BroadcastUint16x8(64)

	// Fast path 2: horizontal half-pel (64,64), arbitrary vertical tap —
	// saves 2 widening multiplies + add + shift per 16 pixels per row.
	if hx0 == 64 && hx1 == 64 {
		if w == 16 {
			prev := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(src[1:17])))
			s, d := src[ss:], dst
			for range h {
				cur := archsimd.LoadUint8x16Array((*[16]byte)(s[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(s[1:17])))
				outLo := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
				outHi := prev.HiToLo().MulWidenLo(cy0).Add(cur.HiToLo().MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
				outLo.ReshapeToUint8s().ConcatEven(outHi.ReshapeToUint8s()).StoreArray((*[16]byte)(d[0:16]))
				prev = cur
				if len(s) > ss {
					s = s[ss:]
				}
				if len(d) > ds {
					d = d[ds:]
				}
			}
			return true
		}
		if w == 8 {
			prev := load8(src[0:]).Average(load8(src[1:]))
			so, do := ss, 0
			for range h {
				cur := load8(src[so:]).Average(load8(src[so+1:]))
				out := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
				storeBytes8(dst[do:], out.TruncToUint8())
				prev = cur
				so += ss
				do += ds
			}
			return true
		}
	}
	// Fast path 3: vertical half-pel (64,64), arbitrary horizontal tap —
	// vertical pass collapses to a single NEON URHADD per 16 pixels.
	if hy0 == 64 && hy1 == 64 {
		if w == 16 {
			r0 := src[0:17]
			a, b := archsimd.LoadUint8x16Array((*[16]byte)(r0[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(r0[1:17]))
			pLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
			pHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
			prev := pLo.ReshapeToUint8s().ConcatEven(pHi.ReshapeToUint8s())
			so, do := ss, 0
			for range h {
				row := src[so : so+17]
				a, b = archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
				cLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
				cHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
				cur := cLo.ReshapeToUint8s().ConcatEven(cHi.ReshapeToUint8s())
				prev.Average(cur).StoreArray((*[16]byte)(dst[do : do+16]))
				prev = cur
				so += ss
				do += ds
			}
			return true
		}
		if w == 8 {
			a, b := load8(src[0:]), load8(src[1:])
			prev := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			so, do := ss, 0
			for range h {
				a, b = load8(src[so:]), load8(src[so+1:])
				cur := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
				storeBytes8(dst[do:], prev.Average(cur))
				prev = cur
				so += ss
				do += ds
			}
			return true
		}
	}

	if w == 16 {
		a, b := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(src[1:17]))
		prevLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
		prevHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
		s, d := src[ss:], dst
		for range h {
			a, b = archsimd.LoadUint8x16Array((*[16]byte)(s[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(s[1:17]))
			curLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			curHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			outLo := prevLo.MulWidenLo(cy0).Add(curLo.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
			outHi := prevHi.MulWidenLo(cy0).Add(curHi.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
			outLo.ReshapeToUint8s().ConcatEven(outHi.ReshapeToUint8s()).StoreArray((*[16]byte)(d[0:16]))
			prevLo, prevHi = curLo, curHi
			if len(s) > ss {
				s = s[ss:]
			}
			if len(d) > ds {
				d = d[ds:]
			}
		}
		return true
	}
	if w == 8 {
		a0, b0 := load8(src[0:]), load8(src[1:])
		prev := a0.MulWidenLo(cx0).Add(b0.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
		so, do := ss, 0
		y := 0
		for ; y+1 < h; y += 2 {
			r0 := src[so : so+9]
			r1 := src[so+ss : so+ss+9]
			a := archsimd.LoadUint64x2Array(&[2]uint64{
				binary.LittleEndian.Uint64(r0[0:8]),
				binary.LittleEndian.Uint64(r1[0:8]),
			}).ReshapeToUint8s()
			b := archsimd.LoadUint64x2Array(&[2]uint64{
				binary.LittleEndian.Uint64(r0[1:9]),
				binary.LittleEndian.Uint64(r1[1:9]),
			}).ReshapeToUint8s()
			c0 := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			c1 := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			o0 := prev.MulWidenLo(cy0).Add(c0.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			o1 := c0.MulWidenLo(cy0).Add(c1.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			storeBytes8(dst[do:], o0)
			storeBytes8(dst[do+ds:], o1)
			prev = c1
			so += 2 * ss
			do += 2 * ds
		}
		if y < h {
			a, b := load8(src[so:]), load8(src[so+1:])
			cur := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			out := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
			storeBytes8(dst[do:], out.TruncToUint8())
		}
		return true
	}
	if w == 4 {
		a, b := load4(src[0:]), load4(src[1:])
		prev := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
		so, do := ss, 0
		for range h {
			a, b = load4(src[so:]), load4(src[so+1:])
			cur := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			out := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			binary.LittleEndian.PutUint32(dst[do:], out.ReshapeToUint32s().GetElem(0))
			prev = cur
			so += ss
			do += ds
		}
		return true
	}

	for x := 0; x < w; {
		if x+16 <= w {
			r0 := src[x : x+17]
			a, b := archsimd.LoadUint8x16Array((*[16]byte)(r0[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(r0[1:17]))
			prevLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			prevHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			so, do := ss+x, x
			for range h {
				row := src[so : so+17]
				a, b = archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
				curLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
				curHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
				outLo := prevLo.MulWidenLo(cy0).Add(curLo.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
				outHi := prevHi.MulWidenLo(cy0).Add(curHi.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
				outLo.ReshapeToUint8s().ConcatEven(outHi.ReshapeToUint8s()).StoreArray((*[16]byte)(dst[do : do+16]))
				prevLo, prevHi = curLo, curHi
				so += ss
				do += ds
			}
			x += 16
		} else {
			a, b := load8(src[x:]), load8(src[x+1:])
			prev := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			so, do := ss+x, x
			for range h {
				a, b = load8(src[so:]), load8(src[so+1:])
				cur := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
				out := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
				storeBytes8(dst[do:], out.TruncToUint8())
				prev = cur
				so += ss
				do += ds
			}
			x += 8
		}
	}
	return true
}

// FilterBilinear2DPair8x8 performs fused 8x8 bilinear interpolation on two
// chroma planes (U and V) in lockstep, packing 8 bytes of src0 in the low
// 64 bits and 8 bytes of src1 in the high 64 bits of each 128-bit NEON vector.
// Handles 1D horizontal (hy0==128), 1D vertical (hx0==128), and 2D subpel.
func FilterBilinear2DPair8x8(
	src0 []byte, ss0 int, src1 []byte, ss1 int,
	dst0 []byte, ds0 int, dst1 []byte, ds1 int,
	hx0, hx1, hy0, hy1 uint8,
) bool {
	if !Enabled || ss0 < 0 || ss1 < 0 || ds0 < 0 || ds1 < 0 {
		return false
	}
	if int(hx0)+int(hx1) > 128 || int(hy0)+int(hy1) > 128 {
		return false
	}
	if ss0 > 1<<20 || ss1 > 1<<20 || ds0 > 1<<20 || ds1 > 1<<20 {
		return false
	}
	srcNeed0 := 8*ss0 + 9
	srcNeed1 := 8*ss1 + 9
	dstNeed0 := 7*ds0 + 8
	dstNeed1 := 7*ds1 + 8
	if hy1 == 0 {
		srcNeed0 = 7*ss0 + 9
		srcNeed1 = 7*ss1 + 9
	}
	if hx1 == 0 {
		srcNeed0--
		srcNeed1--
	}
	if len(src0) < srcNeed0 || len(src1) < srcNeed1 || len(dst0) < dstNeed0 || len(dst1) < dstNeed1 {
		return false
	}
	if slicesOverlap(src0, dst0, srcNeed0, dstNeed0) || slicesOverlap(src1, dst1, srcNeed1, dstNeed1) ||
		slicesOverlap(src0, dst1, srcNeed0, dstNeed1) || slicesOverlap(src1, dst0, srcNeed1, dstNeed0) {
		return false
	}
	_ = src0[srcNeed0-1]
	_ = src1[srcNeed1-1]
	_ = dst0[dstNeed0-1]
	_ = dst1[dstNeed1-1]

	loadPair := func(a, b []byte) archsimd.Uint8x16 {
		return archsimd.LoadUint64x2Array(&[2]uint64{
			binary.LittleEndian.Uint64(a),
			binary.LittleEndian.Uint64(b),
		}).ReshapeToUint8s()
	}
	storePair := func(a, b []byte, v archsimd.Uint8x16) {
		u := v.ReshapeToUint64s()
		binary.LittleEndian.PutUint64(a, u.GetElem(0))
		binary.LittleEndian.PutUint64(b, u.GetElem(1))
	}

	// 1D horizontal only (hy0 == 128, hy1 == 0).
	if hy1 == 0 {
		if hx0 == 64 && hx1 == 64 {
			so0, so1, do0, do1 := 0, 0, 0, 0
			for range 8 {
				a := loadPair(src0[so0:], src1[so1:])
				b := loadPair(src0[so0+1:], src1[so1+1:])
				storePair(dst0[do0:], dst1[do1:], a.Average(b))
				so0 += ss0
				so1 += ss1
				do0 += ds0
				do1 += ds1
			}
			return true
		}
		cx0, cx1 := archsimd.BroadcastUint8x16(hx0), archsimd.BroadcastUint8x16(hx1)
		bias := archsimd.BroadcastUint16x8(64)
		so0, so1, do0, do1 := 0, 0, 0, 0
		for range 8 {
			a := loadPair(src0[so0:], src1[so1:])
			b := loadPair(src0[so0+1:], src1[so1+1:])
			lo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
			hi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
			storePair(dst0[do0:], dst1[do1:], lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()))
			so0 += ss0
			so1 += ss1
			do0 += ds0
			do1 += ds1
		}
		return true
	}

	// 1D vertical only (hx0 == 128, hx1 == 0).
	if hx1 == 0 {
		if hy0 == 64 && hy1 == 64 {
			prev := loadPair(src0[0:], src1[0:])
			so0, so1, do0, do1 := ss0, ss1, 0, 0
			for range 8 {
				cur := loadPair(src0[so0:], src1[so1:])
				storePair(dst0[do0:], dst1[do1:], prev.Average(cur))
				prev = cur
				so0 += ss0
				so1 += ss1
				do0 += ds0
				do1 += ds1
			}
			return true
		}
		cy0, cy1 := archsimd.BroadcastUint8x16(hy0), archsimd.BroadcastUint8x16(hy1)
		bias := archsimd.BroadcastUint16x8(64)
		prev := loadPair(src0[0:], src1[0:])
		so0, so1, do0, do1 := ss0, ss1, 0, 0
		for range 8 {
			cur := loadPair(src0[so0:], src1[so1:])
			lo := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
			hi := prev.HiToLo().MulWidenLo(cy0).Add(cur.HiToLo().MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
			storePair(dst0[do0:], dst1[do1:], lo.ReshapeToUint8s().ConcatEven(hi.ReshapeToUint8s()))
			prev = cur
			so0 += ss0
			so1 += ss1
			do0 += ds0
			do1 += ds1
		}
		return true
	}

	// 2D half-pel (64,64) x (64,64).
	if hx0 == 64 && hx1 == 64 && hy0 == 64 && hy1 == 64 {
		prev := loadPair(src0[0:], src1[0:]).Average(loadPair(src0[1:], src1[1:]))
		so0, so1, do0, do1 := ss0, ss1, 0, 0
		for range 8 {
			cur := loadPair(src0[so0:], src1[so1:]).Average(loadPair(src0[so0+1:], src1[so1+1:]))
			storePair(dst0[do0:], dst1[do1:], prev.Average(cur))
			prev = cur
			so0 += ss0
			so1 += ss1
			do0 += ds0
			do1 += ds1
		}
		return true
	}

	cx0, cx1 := archsimd.BroadcastUint8x16(hx0), archsimd.BroadcastUint8x16(hx1)
	cy0, cy1 := archsimd.BroadcastUint8x16(hy0), archsimd.BroadcastUint8x16(hy1)
	bias := archsimd.BroadcastUint16x8(64)

	// 2D general dual-plane 16-lane NEON.
	a := loadPair(src0[0:], src1[0:])
	b := loadPair(src0[1:], src1[1:])
	pLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
	pHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
	prev := pLo.ReshapeToUint8s().ConcatEven(pHi.ReshapeToUint8s())
	so0, so1, do0, do1 := ss0, ss1, 0, 0
	for range 8 {
		a = loadPair(src0[so0:], src1[so1:])
		b = loadPair(src0[so0+1:], src1[so1+1:])
		cLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
		cHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7)
		cur := cLo.ReshapeToUint8s().ConcatEven(cHi.ReshapeToUint8s())
		oLo := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
		oHi := prev.HiToLo().MulWidenLo(cy0).Add(cur.HiToLo().MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
		storePair(dst0[do0:], dst1[do1:], oLo.ReshapeToUint8s().ConcatEven(oHi.ReshapeToUint8s()))
		prev = cur
		so0 += ss0
		so1 += ss1
		do0 += ds0
		do1 += ds1
	}
	return true
}

func load4(src []byte) archsimd.Uint8x16 {
	return archsimd.LoadUint32x4Array(&[4]uint32{binary.LittleEndian.Uint32(src)}).ReshapeToUint8s()
}

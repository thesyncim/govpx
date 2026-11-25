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
	if len(src) <= h*ss+w || len(dst) <= (h-1)*ds+w-1 {
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

	if w == 16 {
		r0 := src[0:17]
		a, b := archsimd.LoadUint8x16Array((*[16]byte)(r0[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(r0[1:17]))
		prevLo := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
		prevHi := a.HiToLo().MulWidenLo(cx0).Add(b.HiToLo().MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
		so, do := ss, 0
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
		return true
	}
	if w == 8 {
		a, b := load8(src[0:]), load8(src[1:])
		prev := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
		so, do := ss, 0
		for range h {
			a, b = load8(src[so:]), load8(src[so+1:])
			cur := a.MulWidenLo(cx0).Add(b.MulWidenLo(cx1)).Add(bias).ShiftAllRight(7).TruncToUint8()
			out := prev.MulWidenLo(cy0).Add(cur.MulWidenLo(cy1)).Add(bias).ShiftAllRight(7)
			storeBytes8(dst[do:], out.TruncToUint8())
			prev = cur
			so += ss
			do += ds
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

func load4(src []byte) archsimd.Uint8x16 {
	return archsimd.LoadUint32x4Array(&[4]uint32{binary.LittleEndian.Uint32(src)}).ReshapeToUint8s()
}

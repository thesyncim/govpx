//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// Rolling rows require disjoint input/output and non-overlapping output rows.
func filterVerticalRowsAllowed(src, dst []byte, ds, w, h int) bool {
	if h < 2 || w%8 != 0 || ds < w || len(src) == 0 || len(dst) == 0 {
		return false
	}
	return !slicesOverlap(src, dst, len(src), len(dst))
}
func filterHorizSix16(src []byte, ss int, dst []byte, ds, w, h int, taps []int16) bool {
	if int(taps[0])+int(taps[2])+int(taps[3])+int(taps[5]) > 256 {
		return false
	}
	c0 := archsimd.BroadcastUint8x16(uint8(taps[0]))
	c1 := archsimd.BroadcastUint8x16(uint8(-taps[1]))
	c2 := archsimd.BroadcastUint8x16(uint8(taps[2]))
	c3 := archsimd.BroadcastUint8x16(uint8(taps[3]))
	c4 := archsimd.BroadcastUint8x16(uint8(-taps[4]))
	c5 := archsimd.BroadcastUint8x16(uint8(taps[5]))
	bias := archsimd.BroadcastUint16x8(64)
	for y := range h {
		so, do := y*ss, y*ds
		for x := 0; x < w; x += 16 {
			row := src[so+x : so+x+21]
			v0 := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16]))
			v1 := archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
			v2 := archsimd.LoadUint8x16Array((*[16]byte)(row[2:18]))
			v3 := archsimd.LoadUint8x16Array((*[16]byte)(row[3:19]))
			v4 := archsimd.LoadUint8x16Array((*[16]byte)(row[4:20]))
			v5 := archsimd.LoadUint8x16Array((*[16]byte)(row[5:21]))
			posLo := bias.Add(v0.MulWidenLo(c0)).Add(v2.MulWidenLo(c2)).Add(v3.MulWidenLo(c3)).Add(v5.MulWidenLo(c5))
			negLo := v1.MulWidenLo(c1).Add(v4.MulWidenLo(c4))
			posHi := bias.Add(v0.HiToLo().MulWidenLo(c0)).Add(v2.HiToLo().MulWidenLo(c2)).Add(v3.HiToLo().MulWidenLo(c3)).Add(v5.HiToLo().MulWidenLo(c5))
			negHi := v1.HiToLo().MulWidenLo(c1).Add(v4.HiToLo().MulWidenLo(c4))
			outLo := posLo.SubSaturated(negLo).ShiftAllRight(7).SaturateToUint8()
			outHi := posHi.SubSaturated(negHi).ShiftAllRight(7).SaturateToUint8()
			outLo.ReshapeToUint64s().InterleaveLo(outHi.ReshapeToUint64s()).ReshapeToUint8s().StoreArray((*[16]byte)(dst[do+x : do+x+16]))
		}
	}
	return true
}

func filterVerticalSix8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16) bool {
	if !filterVerticalRowsAllowed(src, dst, ds, w, h) {
		return false
	}
	if w >= 16 && w%16 == 0 && int(taps[0])+int(taps[2])+int(taps[3])+int(taps[5]) <= 256 {
		c0 := archsimd.BroadcastUint8x16(uint8(taps[0]))
		c1 := archsimd.BroadcastUint8x16(uint8(-taps[1]))
		c2 := archsimd.BroadcastUint8x16(uint8(taps[2]))
		c3 := archsimd.BroadcastUint8x16(uint8(taps[3]))
		c4 := archsimd.BroadcastUint8x16(uint8(-taps[4]))
		c5 := archsimd.BroadcastUint8x16(uint8(taps[5]))
		bias := archsimd.BroadcastUint16x8(64)
		for x := 0; x < w; x += 16 {
			v0 := archsimd.LoadUint8x16Array((*[16]byte)(src[0*ss+x : 0*ss+x+16]))
			v1 := archsimd.LoadUint8x16Array((*[16]byte)(src[1*ss+x : 1*ss+x+16]))
			v2 := archsimd.LoadUint8x16Array((*[16]byte)(src[2*ss+x : 2*ss+x+16]))
			v3 := archsimd.LoadUint8x16Array((*[16]byte)(src[3*ss+x : 3*ss+x+16]))
			v4 := archsimd.LoadUint8x16Array((*[16]byte)(src[4*ss+x : 4*ss+x+16]))
			so, do := 5*ss+x, x
			for range h {
				v5 := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				posLo := bias.Add(v0.MulWidenLo(c0)).Add(v2.MulWidenLo(c2)).Add(v3.MulWidenLo(c3)).Add(v5.MulWidenLo(c5))
				negLo := v1.MulWidenLo(c1).Add(v4.MulWidenLo(c4))
				posHi := bias.Add(v0.HiToLo().MulWidenLo(c0)).Add(v2.HiToLo().MulWidenLo(c2)).Add(v3.HiToLo().MulWidenLo(c3)).Add(v5.HiToLo().MulWidenLo(c5))
				negHi := v1.HiToLo().MulWidenLo(c1).Add(v4.HiToLo().MulWidenLo(c4))
				outLo := posLo.SubSaturated(negLo).ShiftAllRight(7).SaturateToUint8()
				outHi := posHi.SubSaturated(negHi).ShiftAllRight(7).SaturateToUint8()
				outLo.ReshapeToUint64s().InterleaveLo(outHi.ReshapeToUint64s()).ReshapeToUint8s().StoreArray((*[16]byte)(dst[do : do+16]))
				v0, v1, v2, v3, v4 = v1, v2, v3, v4, v5
				so += ss
				do += ds
			}
		}
		return true
	}
	c0 := archsimd.BroadcastInt16x8(taps[0])
	c1 := archsimd.BroadcastInt16x8(taps[1])
	c2 := archsimd.BroadcastInt16x8(taps[2])
	c3 := archsimd.BroadcastInt16x8(taps[3])
	c4 := archsimd.BroadcastInt16x8(taps[4])
	c5 := archsimd.BroadcastInt16x8(taps[5])
	bias := archsimd.BroadcastInt16x8(64)
	for x := 0; x < w; x += 8 {
		p0 := widen8(load8(src[0*ss+x:]))
		p1 := widen8(load8(src[1*ss+x:]))
		p2 := widen8(load8(src[2*ss+x:]))
		p3 := widen8(load8(src[3*ss+x:]))
		p4 := widen8(load8(src[4*ss+x:]))
		for y := range h {
			p5 := widen8(load8(src[(y+5)*ss+x:]))
			v := mulAdd16(p1.Mul(c1).Add(bias), p4, c4)
			v = v.AddSaturated(p0.Mul(c0)).AddSaturated(p2.Mul(c2)).AddSaturated(p3.Mul(c3)).AddSaturated(p5.Mul(c5))
			storeBytes8(dst[y*ds+x:], packBytes(filterShift7(v)))
			p0, p1, p2, p3, p4 = p1, p2, p3, p4, p5
		}
	}
	return true
}
func filterVerticalFour8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16) bool {
	if !filterVerticalRowsAllowed(src, dst, ds, w, h) {
		return false
	}
	c0 := archsimd.BroadcastInt16x8(taps[0])
	c1 := archsimd.BroadcastInt16x8(taps[1])
	c2 := archsimd.BroadcastInt16x8(taps[2])
	c3 := archsimd.BroadcastInt16x8(taps[3])
	bias := archsimd.BroadcastInt16x8(64)
	for x := 0; x < w; x += 8 {
		p0 := widen8(load8(src[0*ss+x:]))
		p1 := widen8(load8(src[1*ss+x:]))
		p2 := widen8(load8(src[2*ss+x:]))
		for y := range h {
			p3 := widen8(load8(src[(y+3)*ss+x:]))
			v := mulAdd16(p0.Mul(c0).Add(bias), p3, c3)
			v = v.AddSaturated(p1.Mul(c1)).AddSaturated(p2.Mul(c2))
			storeBytes8(dst[y*ds+x:], packBytes(filterShift7(v)))
			p0, p1, p2 = p1, p2, p3
		}
	}
	return true
}

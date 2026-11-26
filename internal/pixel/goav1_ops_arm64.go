//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

func loopFilterWide(p []byte, q0, step, outer, n, blimit, limit, thresh, kind int) bool {
	return loopFilterByteWide(p, q0, step, outer, n, blimit, limit, thresh, kind)
}

var lfByteRight1 = archsimd.BroadcastInt8x16(-1)
var lfByteRight3 = archsimd.BroadcastInt8x16(-3)

func lfByteShift1(v archsimd.Int8x16) archsimd.Int8x16  { return v.Shift(lfByteRight1) }
func lfByteShift3(v archsimd.Int8x16) archsimd.Int8x16  { return v.Shift(lfByteRight3) }
func lfByteHalf(v archsimd.Uint8x16) archsimd.Uint8x16  { return v.Shift(lfByteRight1) }
func lfByteLE(a, b archsimd.Uint8x16) archsimd.Mask8x16 { return a.LessEqual(b) }
func lfByteGT(a, b archsimd.Uint8x16) archsimd.Mask8x16 { return a.Greater(b) }
func lfTruncSigned(v archsimd.Int16x8) archsimd.Int8x16 { return v.TruncToInt8() }

func lfByteNot(v archsimd.Mask8x16) archsimd.Mask8x16 { return v.Not() }

func widenHi8(v archsimd.Uint8x16) archsimd.Uint16x8 {
	return v.HiToLo().ExtendLo8ToUint16()
}

func sqSumInt32(acc archsimd.Int64x2, v archsimd.Int32x4) archsimd.Int64x2 {
	hi := v.HiToLo()
	return acc.Add(v.MulWidenLo(v)).Add(hi.MulWidenLo(hi))
}

// ProjectionCols16 batches independent row sums before their normalization.
// Each accepted row totals at most 64*255=16320, so int16 conversion is exact.
func ProjectionCols16(src []byte, ss, w, rows, norm int, out []int16) bool {
	if !Enabled || (w != 16 && w != 32 && w != 64) || rows < 0 || ss < 0 || norm < 0 || norm > 14 || len(out) < rows {
		return false
	}
	if rows == 0 {
		return true
	}
	if len(src) < w || (rows > 1 && ss > (len(src)-w)/(rows-1)) {
		return false
	}
	_ = src[(rows-1)*ss+w-1]
	_ = out[rows-1]
	y := 0
	for ; y+4 <= rows; y += 4 {
		a0 := archsimd.Uint16x8{}
		a1 := archsimd.Uint16x8{}
		a2 := archsimd.Uint16x8{}
		a3 := archsimd.Uint16x8{}
		base0 := y * ss
		base1 := base0 + ss
		base2 := base1 + ss
		base3 := base2 + ss
		for x := 0; x < w; x += 16 {
			r0 := archsimd.LoadUint8x16Array((*[16]byte)(src[base0+x : base0+x+16]))
			a0 = a0.Add(r0.ExtendLo8ToUint16()).Add(r0.HiToLo().ExtendLo8ToUint16())
			r1 := archsimd.LoadUint8x16Array((*[16]byte)(src[base1+x : base1+x+16]))
			a1 = a1.Add(r1.ExtendLo8ToUint16()).Add(r1.HiToLo().ExtendLo8ToUint16())
			r2 := archsimd.LoadUint8x16Array((*[16]byte)(src[base2+x : base2+x+16]))
			a2 = a2.Add(r2.ExtendLo8ToUint16()).Add(r2.HiToLo().ExtendLo8ToUint16())
			r3 := archsimd.LoadUint8x16Array((*[16]byte)(src[base3+x : base3+x+16]))
			a3 = a3.Add(r3.ExtendLo8ToUint16()).Add(r3.HiToLo().ExtendLo8ToUint16())
		}
		out[y+0] = int16(a0.ReduceSum()) >> uint(norm)
		out[y+1] = int16(a1.ReduceSum()) >> uint(norm)
		out[y+2] = int16(a2.ReduceSum()) >> uint(norm)
		out[y+3] = int16(a3.ReduceSum()) >> uint(norm)
	}
	for ; y < rows; y++ {
		a0 := archsimd.Uint16x8{}
		base := y * ss
		for x := 0; x < w; x += 16 {
			r0 := archsimd.LoadUint8x16Array((*[16]byte)(src[base+x : base+x+16]))
			a0 = a0.Add(r0.ExtendLo8ToUint16()).Add(r0.HiToLo().ExtendLo8ToUint16())
		}
		out[y] = int16(a0.ReduceSum()) >> uint(norm)
	}
	return true
}

func pack2Rows(p []byte, stride int) archsimd.Uint8x16 {
	lo := binary.LittleEndian.Uint64(p)
	hi := binary.LittleEndian.Uint64(p[stride:])
	return archsimd.BroadcastUint64x2(lo).SetElem(1, hi).ReshapeToUint8s()
}

func sad8x8Slice(src []byte, srcStride int, ref []byte, refStride int) int {
	_ = src[7*srcStride+7]
	_ = ref[7*refStride+7]
	s0 := pack2Rows(src[0:], srcStride)
	r0 := pack2Rows(ref[0:], refStride)
	s1 := pack2Rows(src[2*srcStride:], srcStride)
	r1 := pack2Rows(ref[2*refStride:], refStride)
	s2 := pack2Rows(src[4*srcStride:], srcStride)
	r2 := pack2Rows(ref[4*refStride:], refStride)
	s3 := pack2Rows(src[6*srcStride:], srcStride)
	r3 := pack2Rows(ref[6*refStride:], refStride)
	a0 := widen16(absDiffU8x16(s0, r0)).Add(widen16(absDiffU8x16(s1, r1)))
	a1 := widen16(absDiffU8x16(s2, r2)).Add(widen16(absDiffU8x16(s3, r3)))
	return int(a0.Add(a1).ReduceSum())
}

func sad16ColsDualSlice(src []byte, srcStride int, ref []byte, refStride int, h int) int {
	if h == 16 {
		_ = src[15*srcStride+15]
		_ = ref[15*refStride+15]
		var a0, a1, a2, a3 archsimd.Uint16x8
		so, ro := 0, 0
		ss2, ss3, ss4 := 2*srcStride, 3*srcStride, 4*srcStride
		rs2, rs3, rs4 := 2*refStride, 3*refStride, 4*refStride
		for range 4 {
			a0 = absAcc16(a0, archsimd.LoadUint8x16Array((*[16]byte)(src[so:so+16])), archsimd.LoadUint8x16Array((*[16]byte)(ref[ro:ro+16])))
			a1 = absAcc16(a1, archsimd.LoadUint8x16Array((*[16]byte)(src[so+srcStride:so+srcStride+16])), archsimd.LoadUint8x16Array((*[16]byte)(ref[ro+refStride:ro+refStride+16])))
			a2 = absAcc16(a2, archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss2:so+ss2+16])), archsimd.LoadUint8x16Array((*[16]byte)(ref[ro+rs2:ro+rs2+16])))
			a3 = absAcc16(a3, archsimd.LoadUint8x16Array((*[16]byte)(src[so+ss3:so+ss3+16])), archsimd.LoadUint8x16Array((*[16]byte)(ref[ro+rs3:ro+rs3+16])))
			so += ss4
			ro += rs4
		}
		// 16*16*255 = 65280 <= math.MaxUint16, so Uint16x8.ReduceSum is exact.
		return int(a0.Add(a1).Add(a2.Add(a3)).ReduceSum())
	}
	var even, odd archsimd.Uint16x8
	so, ro := 0, 0
	ss2, rs2 := 2*srcStride, 2*refStride
	for row := 0; row < h; row += 2 {
		even = absAcc16(even, archsimd.LoadUint8x16Array((*[16]byte)(src[so:so+16])), archsimd.LoadUint8x16Array((*[16]byte)(ref[ro:ro+16])))
		odd = absAcc16(odd, archsimd.LoadUint8x16Array((*[16]byte)(src[so+srcStride:so+srcStride+16])), archsimd.LoadUint8x16Array((*[16]byte)(ref[ro+refStride:ro+refStride+16])))
		so += ss2
		ro += rs2
	}
	return sumU16(even) + sumU16(odd)
}

func sad16ColsX4Slice(src, p0, p1, p2, p3 []byte, srcStride, refStride int, h int) (int, int, int, int) {
	var a0, a1, a2, a3 archsimd.Uint16x8
	if h > 0 && (uint(srcStride)|uint(refStride))>>57 == 0 &&
		(h-1)*srcStride+16 <= len(src) &&
		(h-1)*refStride+16 <= len(p0) &&
		(h-1)*refStride+16 <= len(p1) &&
		(h-1)*refStride+16 <= len(p2) &&
		(h-1)*refStride+16 <= len(p3) {
		_ = src[(h-1)*srcStride+15]
		_ = p0[(h-1)*refStride+15]
		_ = p1[(h-1)*refStride+15]
		_ = p2[(h-1)*refStride+15]
		_ = p3[(h-1)*refStride+15]
		if h == 16 {
			var b0, b1, b2, b3 archsimd.Uint16x8
			so, ro := 0, 0
			ss2, rs2 := 2*srcStride, 2*refStride
			for range 8 {
				s0 := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				s1 := archsimd.LoadUint8x16Array((*[16]byte)(src[so+srcStride : so+srcStride+16]))
				a0 = absAcc16(a0, s0, archsimd.LoadUint8x16Array((*[16]byte)(p0[ro:ro+16])))
				b0 = absAcc16(b0, s1, archsimd.LoadUint8x16Array((*[16]byte)(p0[ro+refStride:ro+refStride+16])))
				a1 = absAcc16(a1, s0, archsimd.LoadUint8x16Array((*[16]byte)(p1[ro:ro+16])))
				b1 = absAcc16(b1, s1, archsimd.LoadUint8x16Array((*[16]byte)(p1[ro+refStride:ro+refStride+16])))
				a2 = absAcc16(a2, s0, archsimd.LoadUint8x16Array((*[16]byte)(p2[ro:ro+16])))
				b2 = absAcc16(b2, s1, archsimd.LoadUint8x16Array((*[16]byte)(p2[ro+refStride:ro+refStride+16])))
				a3 = absAcc16(a3, s0, archsimd.LoadUint8x16Array((*[16]byte)(p3[ro:ro+16])))
				b3 = absAcc16(b3, s1, archsimd.LoadUint8x16Array((*[16]byte)(p3[ro+refStride:ro+refStride+16])))
				so += ss2
				ro += rs2
			}
			return int(a0.Add(b0).ReduceSum()), int(a1.Add(b1).ReduceSum()), int(a2.Add(b2).ReduceSum()), int(a3.Add(b3).ReduceSum())
		}
		so, ro := 0, 0
		for range h {
			s := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
			a0 = absAcc16(a0, s, archsimd.LoadUint8x16Array((*[16]byte)(p0[ro:ro+16])))
			a1 = absAcc16(a1, s, archsimd.LoadUint8x16Array((*[16]byte)(p1[ro:ro+16])))
			a2 = absAcc16(a2, s, archsimd.LoadUint8x16Array((*[16]byte)(p2[ro:ro+16])))
			a3 = absAcc16(a3, s, archsimd.LoadUint8x16Array((*[16]byte)(p3[ro:ro+16])))
			so += srcStride
			ro += refStride
		}
		return sumU16(a0), sumU16(a1), sumU16(a2), sumU16(a3)
	}
	for row := range h {
		o := row * refStride
		s := archsimd.LoadUint8x16(src[row*srcStride:])
		a0 = absAcc16(a0, s, archsimd.LoadUint8x16(p0[o:]))
		a1 = absAcc16(a1, s, archsimd.LoadUint8x16(p1[o:]))
		a2 = absAcc16(a2, s, archsimd.LoadUint8x16(p2[o:]))
		a3 = absAcc16(a3, s, archsimd.LoadUint8x16(p3[o:]))
	}
	return sumU16(a0), sumU16(a1), sumU16(a2), sumU16(a3)
}

func absDiffU8x16(a, b archsimd.Uint8x16) archsimd.Uint8x16 {
	return a.SubSaturated(b).Or(b.SubSaturated(a))
}

func widen16(absd archsimd.Uint8x16) archsimd.Uint16x8 {
	return absd.ExtendLo8ToUint16().Add(absd.HiToLo().ExtendLo8ToUint16())
}

func absAcc16(acc archsimd.Uint16x8, s, r archsimd.Uint8x16) archsimd.Uint16x8 {
	return acc.Add(widen16(absDiffU8x16(s, r)))
}

func sumU16(acc archsimd.Uint16x8) int {
	return int(acc.ExtendLo4ToUint32().Add(acc.HiToLo().ExtendLo4ToUint32()).ReduceSum())
}

func sad16LimitSlice(src []byte, ss int, ref []byte, rs, h, limit int) int {
	var sum archsimd.Uint16x8
	for y := range h {
		a := archsimd.LoadUint8x16(src[y*ss:])
		b := archsimd.LoadUint8x16(ref[y*rs:])
		sum = sum.Add(absPairs(a, b))
		if value := sumPairs(sum); value > limit {
			return value
		}
	}
	return sumPairs(sum)
}

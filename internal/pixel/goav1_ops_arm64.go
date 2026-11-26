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
	y := 0
	for ; y+4 <= rows; y += 4 {
		a0 := archsimd.Uint16x8{}
		a1 := archsimd.Uint16x8{}
		a2 := archsimd.Uint16x8{}
		a3 := archsimd.Uint16x8{}
		for x := 0; x < w; x += 16 {
			r0 := archsimd.LoadUint8x16(src[(y+0)*ss+x:])
			a0 = a0.Add(r0.ExtendLo8ToUint16()).Add(highBytes(r0).ExtendLo8ToUint16())
			r1 := archsimd.LoadUint8x16(src[(y+1)*ss+x:])
			a1 = a1.Add(r1.ExtendLo8ToUint16()).Add(highBytes(r1).ExtendLo8ToUint16())
			r2 := archsimd.LoadUint8x16(src[(y+2)*ss+x:])
			a2 = a2.Add(r2.ExtendLo8ToUint16()).Add(highBytes(r2).ExtendLo8ToUint16())
			r3 := archsimd.LoadUint8x16(src[(y+3)*ss+x:])
			a3 = a3.Add(r3.ExtendLo8ToUint16()).Add(highBytes(r3).ExtendLo8ToUint16())
		}
		out[y+0] = int16(a0.ReduceSum()) >> uint(norm)
		out[y+1] = int16(a1.ReduceSum()) >> uint(norm)
		out[y+2] = int16(a2.ReduceSum()) >> uint(norm)
		out[y+3] = int16(a3.ReduceSum()) >> uint(norm)
	}
	for ; y < rows; y++ {
		a0 := archsimd.Uint16x8{}
		for x := 0; x < w; x += 16 {
			r0 := archsimd.LoadUint8x16(src[(y+0)*ss+x:])
			a0 = a0.Add(r0.ExtendLo8ToUint16()).Add(highBytes(r0).ExtendLo8ToUint16())
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
	acc := archsimd.BroadcastUint16x8(0)
	for row := 0; row < 8; row += 2 {
		s := pack2Rows(src[row*srcStride:], srcStride)
		r := pack2Rows(ref[row*refStride:], refStride)
		acc = acc.Add(widen16(absDiffU8x16(s, r)))
	}
	return int(acc.ReduceSum())
}

func sad16ColsDualSlice(src []byte, srcStride int, ref []byte, refStride int, h int) int {
	even := archsimd.BroadcastUint16x8(0)
	odd := archsimd.BroadcastUint16x8(0)
	for row := 0; row < h; row += 2 {
		even = absAcc16(even, archsimd.LoadUint8x16(src[row*srcStride:]), archsimd.LoadUint8x16(ref[row*refStride:]))
		odd = absAcc16(odd, archsimd.LoadUint8x16(src[(row+1)*srcStride:]), archsimd.LoadUint8x16(ref[(row+1)*refStride:]))
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
		for row := range h {
			s := archsimd.LoadUint8x16Array((*[16]byte)(src[0:16]))
			a0 = absAcc16(a0, s, archsimd.LoadUint8x16Array((*[16]byte)(p0[0:16])))
			a1 = absAcc16(a1, s, archsimd.LoadUint8x16Array((*[16]byte)(p1[0:16])))
			a2 = absAcc16(a2, s, archsimd.LoadUint8x16Array((*[16]byte)(p2[0:16])))
			a3 = absAcc16(a3, s, archsimd.LoadUint8x16Array((*[16]byte)(p3[0:16])))
			if row+1 < h {
				src = src[srcStride:]
				p0 = p0[refStride:]
				p1 = p1[refStride:]
				p2 = p2[refStride:]
				p3 = p3[refStride:]
			}
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

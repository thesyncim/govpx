//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

func load8(b []byte) archsimd.Uint8x16 {
	return archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(b)).ReshapeToUint8s()
}
func storeBytes8(b []byte, v archsimd.Uint8x16) {
	binary.LittleEndian.PutUint64(b, v.ReshapeToUint64s().GetElem(0))
}

func widen8(v archsimd.Uint8x16) archsimd.Int16x8 { return v.ExtendLo8ToUint16().BitsToInt16() }
func high4(v archsimd.Int16x8) archsimd.Int32x4   { return v.HiToLo().ExtendLo4ToInt32() }
func mac(lo, hi archsimd.Int32x4, v, c archsimd.Int16x8) (archsimd.Int32x4, archsimd.Int32x4) {
	return lo.Add(v.MulWidenLo(c)), hi.Add(v.HiToLo().MulWidenLo(c))
}
func packBytes(v archsimd.Int16x8) archsimd.Uint8x16 { return v.SaturateToUint8() }

// Same narrowing layout as goav1's planePackPairI16/planeNarrowWord.
func store8(b []byte, lo, hi archsimd.Int32x4) {
	l := lo.SaturateToInt16().ToBits().ReshapeToUint64s()
	h := hi.SaturateToInt16().ToBits().ReshapeToUint64s()
	storeBytes8(b, l.InterleaveLo(h).ReshapeToUint16s().BitsToInt16().SaturateToUint8())
}
func sadAdd(acc archsimd.Uint32x4, a, b archsimd.Uint8x16) archsimd.Uint32x4 {
	d := a.Max(b).Sub(a.Min(b))
	lo := d.ExtendLo8ToUint16()
	hi := d.HiToLo().ExtendLo8ToUint16()
	return acc.Add(lo.ExtendLo4ToUint32()).Add(lo.HiToLo().ExtendLo4ToUint32()).Add(hi.ExtendLo4ToUint32()).Add(hi.HiToLo().ExtendLo4ToUint32())
}
func sumSquares(v archsimd.Int32x4) uint64 {
	s := sqSumInt32(archsimd.Int64x2{}, v)
	return uint64(s.GetElem(0)) + uint64(s.GetElem(1))
}
func Store4(s []int16, v archsimd.Int32x4) {
	put4(s, v.TruncToInt16().ToBits().ReshapeToUint64s().GetElem(0))
}
func StorePixels4(s []byte, v archsimd.Int32x4) {
	binary.LittleEndian.PutUint32(s, v.SaturateToInt16().SaturateToUint8().ReshapeToUint32s().GetElem(0))
}
func sum32(v archsimd.Int32x4) int32    { return v.ReduceSum() }
func sumU32(v archsimd.Uint32x4) uint32 { return v.ReduceSum() }
func Max4(v archsimd.Int32x4) int32     { return v.ReduceMax() }

// Four int16 outputs occupy precisely the eight bytes written here. This
// store is unaligned-safe on amd64 and arm64 and stays within the Go slice.
func put4(out []int16, v uint64) {
	_ = out[3]
	out[0] = int16(v)
	out[1] = int16(v >> 16)
	out[2] = int16(v >> 32)
	out[3] = int16(v >> 48)
}

func highBytes(v archsimd.Uint8x16) archsimd.Uint8x16 { return v.HiToLo() }

func permute16(v, i archsimd.Uint8x16) archsimd.Uint8x16 { return v.LookupOrZero(i) }

func highU16(v archsimd.Uint16x8) archsimd.Uint16x8 { return v.HiToLo() }

// Keep the eight-tap vertical window in registers. Adjacent output rows
// share seven input rows; only the incoming row needs another memory load.
func filterVertical8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16) bool {
	if len(taps) != 8 || w <= 0 || w%8 != 0 || h <= 0 || ss < 0 || ds < 0 {
		return false
	}
	_ = src[(h+6)*ss+w-1]
	_ = dst[(h-1)*ds+w-1]
	c0 := archsimd.BroadcastInt16x8(taps[0])
	c1 := archsimd.BroadcastInt16x8(taps[1])
	c2 := archsimd.BroadcastInt16x8(taps[2])
	c3 := archsimd.BroadcastInt16x8(taps[3])
	c4 := archsimd.BroadcastInt16x8(taps[4])
	c5 := archsimd.BroadcastInt16x8(taps[5])
	c6 := archsimd.BroadcastInt16x8(taps[6])
	c7 := archsimd.BroadcastInt16x8(taps[7])
	bias := archsimd.BroadcastInt32x4(64)
	for x := 0; x < w; x += 8 {
		p0 := widen8(load8(src[0*ss+x:]))
		p1 := widen8(load8(src[1*ss+x:]))
		p2 := widen8(load8(src[2*ss+x:]))
		p3 := widen8(load8(src[3*ss+x:]))
		p4 := widen8(load8(src[4*ss+x:]))
		p5 := widen8(load8(src[5*ss+x:]))
		p6 := widen8(load8(src[6*ss+x:]))
		for y := range h {
			p7 := widen8(load8(src[(y+7)*ss+x:]))
			lo, hi := bias, bias
			lo, hi = mac(lo, hi, p0, c0)
			lo, hi = mac(lo, hi, p1, c1)
			lo, hi = mac(lo, hi, p2, c2)
			lo, hi = mac(lo, hi, p3, c3)
			lo, hi = mac(lo, hi, p4, c4)
			lo, hi = mac(lo, hi, p5, c5)
			lo, hi = mac(lo, hi, p6, c6)
			lo, hi = mac(lo, hi, p7, c7)
			store8(dst[y*ds+x:], lo.ShiftAllRight(7), hi.ShiftAllRight(7))
			p0, p1, p2, p3, p4, p5, p6 = p1, p2, p3, p4, p5, p6, p7
		}
	}
	return true
}

func RoundTransform4(v archsimd.Int32x4) archsimd.Int32x4 {
	return v.Add(archsimd.BroadcastInt32x4(8192)).ShiftAllRight(14)
}
func WrapTransform4(v archsimd.Int32x4) archsimd.Int32x4 { return v.TruncToInt16().ExtendLo4ToInt32() }

func mulAdd16(sum, a, b archsimd.Int16x8) archsimd.Int16x8 { return a.MulAdd(b, sum) }

func filterShift7(v archsimd.Int16x8) archsimd.Int16x8 { return v.ShiftAllRight(7) }

func filterVerticalNarrow8(src []byte, ss int, dst []byte, ds, w, h int, taps []int16, mode int) bool {
	if len(taps) != 8 || w <= 0 || w%8 != 0 || h <= 0 || ss < 0 || ds < 0 {
		return false
	}
	_ = src[(h+6)*ss+w-1]
	_ = dst[(h-1)*ds+w-1]
	c0 := archsimd.BroadcastInt16x8(taps[0])
	c1 := archsimd.BroadcastInt16x8(taps[1])
	c2 := archsimd.BroadcastInt16x8(taps[2])
	c3 := archsimd.BroadcastInt16x8(taps[3])
	c4 := archsimd.BroadcastInt16x8(taps[4])
	c5 := archsimd.BroadcastInt16x8(taps[5])
	c6 := archsimd.BroadcastInt16x8(taps[6])
	c7 := archsimd.BroadcastInt16x8(taps[7])
	bias := archsimd.BroadcastInt16x8(64)
	for x := 0; x < w; x += 8 {
		p0 := widen8(load8(src[0*ss+x:]))
		p1 := widen8(load8(src[1*ss+x:]))
		p2 := widen8(load8(src[2*ss+x:]))
		p3 := widen8(load8(src[3*ss+x:]))
		p4 := widen8(load8(src[4*ss+x:]))
		p5 := widen8(load8(src[5*ss+x:]))
		p6 := widen8(load8(src[6*ss+x:]))
		for y := range h {
			p7 := widen8(load8(src[(y+7)*ss+x:]))
			acc := bias
			if mode == 1 {
				acc = mulAdd16(acc, p0, c0)
				acc = mulAdd16(acc, p2, c2)
				acc = mulAdd16(acc, p5, c5)
				acc = mulAdd16(acc, p7, c7)
				acc = acc.AddSaturated(p1.Mul(c1)).AddSaturated(p3.Mul(c3)).AddSaturated(p4.Mul(c4)).AddSaturated(p6.Mul(c6))
			} else {
				acc = mulAdd16(acc, p0, c0)
				acc = mulAdd16(acc, p1, c1)
				acc = mulAdd16(acc, p6, c6)
				acc = mulAdd16(acc, p7, c7)
				acc = acc.AddSaturated(p2.Mul(c2)).AddSaturated(p3.Mul(c3)).AddSaturated(p4.Mul(c4)).AddSaturated(p5.Mul(c5))
			}
			storeBytes8(dst[y*ds+x:], packBytes(filterShift7(acc)))
			p0, p1, p2, p3, p4, p5, p6 = p1, p2, p3, p4, p5, p6, p7
		}
	}
	return true
}

func MulAdd4(a, b, c archsimd.Int32x4) archsimd.Int32x4 { return a.MulAdd(b, c) }

func fpMulHigh16(a, b archsimd.Int16x8) archsimd.Int16x8 {
	lo := a.MulWidenLo(b).ToBits().ReshapeToUint16s()
	hi := a.HiToLo().MulWidenLo(b.HiToLo()).ToBits().ReshapeToUint16s()
	return lo.ConcatOdd(hi).BitsToInt16()
}
func fpReduceMax16(v archsimd.Int16x8) int { return int(v.ReduceMax()) }

func zipBytesLo(a, b archsimd.Uint8x16) archsimd.Uint16x8 {
	return a.InterleaveLo(b).ReshapeToUint16s()
}

func zipBytesHi(a, b archsimd.Uint8x16) archsimd.Uint16x8 {
	return a.InterleaveHi(b).ReshapeToUint16s()
}

func square8(v archsimd.Int16x8) archsimd.Int32x4 {
	hi := v.HiToLo()
	return v.MulWidenLo(v).Add(hi.MulWidenLo(hi))
}
func sumDiff16(v archsimd.Int16x8) archsimd.Int32x4 {
	return v.ExtendLo4ToInt32().Add(v.HiToLo().ExtendLo4ToInt32())
}

var lfRight3 = archsimd.BroadcastInt16x8(-3)
var lfRight1 = archsimd.BroadcastInt16x8(-1)

func lfShift3(v archsimd.Int16x8) archsimd.Int16x8 { return v.Shift(lfRight3) }
func lfShift1(v archsimd.Int16x8) archsimd.Int16x8 { return v.Shift(lfRight1) }

func vp8MulHighU16(a, b archsimd.Uint16x8) archsimd.Uint16x8 {
	lo := a.MulWidenLo(b).ReshapeToUint16s()
	hi := a.HiToLo().MulWidenLo(b.HiToLo()).ReshapeToUint16s()
	return lo.ConcatOdd(hi)
}

// ARM already narrows with one native saturated instruction.
func fpClassIndices(v archsimd.Int16x8) archsimd.Uint8x16 { return v.SaturateToUint8() }

func satInt16ToUint8(v archsimd.Int16x8) archsimd.Uint8x16 { return v.SaturateToUint8() }

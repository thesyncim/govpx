//go:build goexperiment.simd && arm64 && !purego

package dsp

import (
	"encoding/binary"
	"simd/archsimd"

	"github.com/thesyncim/govpx/internal/pixel"
)

// Eight-line layout follows goav1's int16 transform pipeline; VP9's
// rotations and wrapping stages remain distinct from AV1's saturation.
func idct8Rotate16(a, b archsimd.Int16x8, ca, cb int64) archsimd.Int16x8 {
	ka, kb := archsimd.BroadcastInt16x8(int16(ca)), archsimd.BroadcastInt16x8(int16(cb))
	lo := pixel.RoundTransform4(a.MulWidenLo(ka).Add(b.MulWidenLo(kb))).TruncToInt16().ToBits().ReshapeToUint64s()
	hi := pixel.RoundTransform4(a.HiToLo().MulWidenLo(ka).Add(b.HiToLo().MulWidenLo(kb))).TruncToInt16().ToBits().ReshapeToUint64s()
	return lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16()
}

// idct8PairCos16 computes (a*cospi16 + b*cospi16, a*cospi16 - b*cospi16)
// sharing the two widen-multiplies instead of computing them twice.
func idct8PairCos16(a, b archsimd.Int16x8) (archsimd.Int16x8, archsimd.Int16x8) {
	k := archsimd.BroadcastInt16x8(int16(cospi16_64))
	alo, blo := a.MulWidenLo(k), b.MulWidenLo(k)
	ahi, bhi := a.HiToLo().MulWidenLo(k), b.HiToLo().MulWidenLo(k)
	sumLo := pixel.RoundTransform4(alo.Add(blo)).TruncToInt16().ToBits().ReshapeToUint64s()
	sumHi := pixel.RoundTransform4(ahi.Add(bhi)).TruncToInt16().ToBits().ReshapeToUint64s()
	diffLo := pixel.RoundTransform4(alo.Sub(blo)).TruncToInt16().ToBits().ReshapeToUint64s()
	diffHi := pixel.RoundTransform4(ahi.Sub(bhi)).TruncToInt16().ToBits().ReshapeToUint64s()
	return sumLo.InterleaveLo(sumHi).ReshapeToUint16s().BitsToInt16(),
		diffLo.InterleaveLo(diffHi).ReshapeToUint16s().BitsToInt16()
}

func idct8Lines16(a0, a1, a2, a3, a4, a5, a6, a7 archsimd.Int16x8) (archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8) {
	s4 := idct8Rotate16(a1, a7, cospi28_64, -cospi4_64)
	s7 := idct8Rotate16(a1, a7, cospi4_64, cospi28_64)
	s5 := idct8Rotate16(a5, a3, cospi12_64, -cospi20_64)
	s6 := idct8Rotate16(a5, a3, cospi20_64, cospi12_64)
	t0, t1 := idct8PairCos16(a0, a4)
	t2 := idct8Rotate16(a2, a6, cospi24_64, -cospi8_64)
	t3 := idct8Rotate16(a2, a6, cospi8_64, cospi24_64)
	t4, t5, t6, t7 := s4.Add(s5), s4.Sub(s5), s7.Sub(s6), s6.Add(s7)
	u0, u1, u2, u3 := t0.Add(t3), t1.Add(t2), t1.Sub(t2), t0.Sub(t3)
	u6, u5 := idct8PairCos16(t6, t5)
	return u0.Add(t7), u1.Add(u6), u2.Add(u5), u3.Add(t4), u3.Sub(t4), u2.Sub(u5), u1.Sub(u6), u0.Sub(t7)
}

// idct8Lines16Top4 transforms 8 rows whose high 4 lanes are all zero
// (rows==4 after transpose, i.e. EOB <= 10 top-left 4x4), avoiding all
// HiToLo() multiplies in pass 1.
func idct8Lines16Top4(a0, a1, a2, a3, a4, a5, a6, a7 archsimd.Int16x8) (archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8) {
	var zero64 archsimd.Uint64x2
	rotLo := func(a, b archsimd.Int16x8, ca, cb int64) archsimd.Int16x8 {
		ka, kb := archsimd.BroadcastInt16x8(int16(ca)), archsimd.BroadcastInt16x8(int16(cb))
		return pixel.RoundTransform4(a.MulWidenLo(ka).Add(b.MulWidenLo(kb))).TruncToInt16().ToBits().ReshapeToUint64s().InterleaveLo(zero64).ReshapeToUint16s().BitsToInt16()
	}
	pairCos16Lo := func(a, b archsimd.Int16x8) (archsimd.Int16x8, archsimd.Int16x8) {
		k := archsimd.BroadcastInt16x8(int16(cospi16_64))
		alo, blo := a.MulWidenLo(k), b.MulWidenLo(k)
		sum := pixel.RoundTransform4(alo.Add(blo)).TruncToInt16().ToBits().ReshapeToUint64s().InterleaveLo(zero64).ReshapeToUint16s().BitsToInt16()
		diff := pixel.RoundTransform4(alo.Sub(blo)).TruncToInt16().ToBits().ReshapeToUint64s().InterleaveLo(zero64).ReshapeToUint16s().BitsToInt16()
		return sum, diff
	}
	s4 := rotLo(a1, a7, cospi28_64, -cospi4_64)
	s7 := rotLo(a1, a7, cospi4_64, cospi28_64)
	s5 := rotLo(a5, a3, cospi12_64, -cospi20_64)
	s6 := rotLo(a5, a3, cospi20_64, cospi12_64)
	t0, t1 := pairCos16Lo(a0, a4)
	t2 := rotLo(a2, a6, cospi24_64, -cospi8_64)
	t3 := rotLo(a2, a6, cospi8_64, cospi24_64)
	t4, t5, t6, t7 := s4.Add(s5), s4.Sub(s5), s7.Sub(s6), s6.Add(s7)
	u0, u1, u2, u3 := t0.Add(t3), t1.Add(t2), t1.Sub(t2), t0.Sub(t3)
	u6, u5 := pairCos16Lo(t6, t5)
	return u0.Add(t7), u1.Add(u6), u2.Add(u5), u3.Add(t4), u3.Sub(t4), u2.Sub(u5), u1.Sub(u6), u0.Sub(t7)
}

var idctRight5 = archsimd.BroadcastInt16x8(-5)
var idctRight4 = archsimd.BroadcastInt16x8(-4)

func idct8Store16(dst []byte, v archsimd.Int16x8) {
	_ = dst[7]
	// Split the rounding bit so +16 never overflows signed int16.
	v = v.Shift(idctRight5).Add(v.Shift(idctRight4).And(archsimd.BroadcastInt16x8(1)))
	p := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(dst)).ReshapeToUint8s().ExtendLo8ToUint16().BitsToInt16()
	binary.LittleEndian.PutUint64(dst, v.Add(p).SaturateToUint8().ReshapeToUint64s().GetElem(0))
}
func inverseDCT8ARM(input []int16, dst []byte, stride, rows int) bool {
	if (rows != 4 && rows != 8) || len(input) < 8*rows || stride < 8 || len(dst) < 7*stride+8 {
		return false
	}
	a0, a1, a2, a3 := archsimd.LoadInt16x8(input), archsimd.LoadInt16x8(input[8:]), archsimd.LoadInt16x8(input[16:]), archsimd.LoadInt16x8(input[24:])
	var a4, a5, a6, a7 archsimd.Int16x8
	if rows == 8 {
		a4, a5, a6, a7 = archsimd.LoadInt16x8(input[32:]), archsimd.LoadInt16x8(input[40:]), archsimd.LoadInt16x8(input[48:]), archsimd.LoadInt16x8(input[56:])
		a0, a1, a2, a3, a4, a5, a6, a7 = pixel.Transpose8Int16(a0, a1, a2, a3, a4, a5, a6, a7)
		a0, a1, a2, a3, a4, a5, a6, a7 = idct8Lines16(a0, a1, a2, a3, a4, a5, a6, a7)
	} else {
		a0, a1, a2, a3, a4, a5, a6, a7 = pixel.Transpose8Int16(a0, a1, a2, a3, a4, a5, a6, a7)
		a0, a1, a2, a3, a4, a5, a6, a7 = idct8Lines16Top4(a0, a1, a2, a3, a4, a5, a6, a7)
	}
	a0, a1, a2, a3, a4, a5, a6, a7 = pixel.Transpose8Int16(a0, a1, a2, a3, a4, a5, a6, a7)
	a0, a1, a2, a3, a4, a5, a6, a7 = idct8Lines16(a0, a1, a2, a3, a4, a5, a6, a7)
	idct8Store16(dst, a0)
	idct8Store16(dst[stride:], a1)
	idct8Store16(dst[2*stride:], a2)
	idct8Store16(dst[3*stride:], a3)
	idct8Store16(dst[4*stride:], a4)
	idct8Store16(dst[5*stride:], a5)
	idct8Store16(dst[6*stride:], a6)
	idct8Store16(dst[7*stride:], a7)
	return true
}

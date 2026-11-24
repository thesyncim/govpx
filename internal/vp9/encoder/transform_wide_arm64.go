//go:build goexperiment.simd && arm64 && !purego

package encoder

import (
	"simd/archsimd"

	"github.com/thesyncim/govpx/internal/pixel"
)

var (
	arm64Cospi4_16     = archsimd.BroadcastInt16x8(int16(fdctCospi4_64))
	arm64NegCospi4_16  = archsimd.BroadcastInt16x8(int16(-fdctCospi4_64))
	arm64Cospi8_16     = archsimd.BroadcastInt16x8(int16(fdctCospi8_64))
	arm64NegCospi8_16  = archsimd.BroadcastInt16x8(int16(-fdctCospi8_64))
	arm64Cospi12_16    = archsimd.BroadcastInt16x8(int16(fdctCospi12_64))
	arm64Cospi16_16    = archsimd.BroadcastInt16x8(int16(fdctCospi16_64))
	arm64Cospi20_16    = archsimd.BroadcastInt16x8(int16(fdctCospi20_64))
	arm64NegCospi20_16 = archsimd.BroadcastInt16x8(int16(-fdctCospi20_64))
	arm64Cospi24_16    = archsimd.BroadcastInt16x8(int16(fdctCospi24_64))
	arm64Cospi28_16    = archsimd.BroadcastInt16x8(int16(fdctCospi28_64))

	arm64Cospi4_32     = archsimd.BroadcastInt32x4(fdctCospi4_64)
	arm64NegCospi4_32  = archsimd.BroadcastInt32x4(-fdctCospi4_64)
	arm64Cospi12_32    = archsimd.BroadcastInt32x4(fdctCospi12_64)
	arm64Cospi20_32    = archsimd.BroadcastInt32x4(fdctCospi20_64)
	arm64NegCospi20_32 = archsimd.BroadcastInt32x4(-fdctCospi20_64)
	arm64Cospi28_32    = archsimd.BroadcastInt32x4(fdctCospi28_64)
)

func forwardDCTWideGo(input []int16, stride int, output []int16, n int) bool {
	if n == 8 {
		return forwardDCT8ARM64(input, stride, output)
	}
	return false
}

func packInt32x4Pair(lo, hi archsimd.Int32x4) archsimd.Int16x8 {
	return lo.ToBits().ReshapeToUint16s().ConcatEven(hi.ToBits().ReshapeToUint16s()).BitsToInt16()
}

func fdctMul1Round16(a, c archsimd.Int16x8) archsimd.Int16x8 {
	lo := pixel.RoundTransform4(a.MulWidenLo(c))
	hi := pixel.RoundTransform4(a.HiToLo().MulWidenLo(c))
	return packInt32x4Pair(lo, hi)
}

func fdctMul2Round16(a, c1, b, c2 archsimd.Int16x8) archsimd.Int16x8 {
	lo := pixel.RoundTransform4(a.MulWidenLo(c1).Add(b.MulWidenLo(c2)))
	hi := pixel.RoundTransform4(a.HiToLo().MulWidenLo(c1).Add(b.HiToLo().MulWidenLo(c2)))
	return packInt32x4Pair(lo, hi)
}

func fdctDiv2Pack(lo, hi archsimd.Int32x4) archsimd.Int16x8 {
	lo = lo.Sub(lo.ShiftAllRight(31)).ShiftAllRight(1)
	hi = hi.Sub(hi.ShiftAllRight(31)).ShiftAllRight(1)
	return packInt32x4Pair(lo, hi)
}

func transpose8x8Int16(r0, r1, r2, r3, r4, r5, r6, r7 archsimd.Int16x8) (
	archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8,
	archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8, archsimd.Int16x8,
) {
	t0 := r0.InterleaveLo(r1).ToBits().ReshapeToUint32s()
	t1 := r0.InterleaveHi(r1).ToBits().ReshapeToUint32s()
	t2 := r2.InterleaveLo(r3).ToBits().ReshapeToUint32s()
	t3 := r2.InterleaveHi(r3).ToBits().ReshapeToUint32s()
	t4 := r4.InterleaveLo(r5).ToBits().ReshapeToUint32s()
	t5 := r4.InterleaveHi(r5).ToBits().ReshapeToUint32s()
	t6 := r6.InterleaveLo(r7).ToBits().ReshapeToUint32s()
	t7 := r6.InterleaveHi(r7).ToBits().ReshapeToUint32s()

	u0 := t0.InterleaveLo(t2).ReshapeToUint64s()
	u1 := t0.InterleaveHi(t2).ReshapeToUint64s()
	u2 := t1.InterleaveLo(t3).ReshapeToUint64s()
	u3 := t1.InterleaveHi(t3).ReshapeToUint64s()
	u4 := t4.InterleaveLo(t6).ReshapeToUint64s()
	u5 := t4.InterleaveHi(t6).ReshapeToUint64s()
	u6 := t5.InterleaveLo(t7).ReshapeToUint64s()
	u7 := t5.InterleaveHi(t7).ReshapeToUint64s()

	o0 := u0.InterleaveLo(u4).ReshapeToUint16s().BitsToInt16()
	o1 := u0.InterleaveHi(u4).ReshapeToUint16s().BitsToInt16()
	o2 := u1.InterleaveLo(u5).ReshapeToUint16s().BitsToInt16()
	o3 := u1.InterleaveHi(u5).ReshapeToUint16s().BitsToInt16()
	o4 := u2.InterleaveLo(u6).ReshapeToUint16s().BitsToInt16()
	o5 := u2.InterleaveHi(u6).ReshapeToUint16s().BitsToInt16()
	o6 := u3.InterleaveLo(u7).ReshapeToUint16s().BitsToInt16()
	o7 := u3.InterleaveHi(u7).ReshapeToUint16s().BitsToInt16()
	return o0, o1, o2, o3, o4, o5, o6, o7
}

// forwardDCT8ARM64 computes the 8x8 forward DCT across all 8 lanes in NEON
// Int16x8 registers with in-register 8x8 transposes and zero stack spill buffer.
// Bit-exact with forwardDCT8Go for all residuals in [-255, 255].
func forwardDCT8ARM64(input []int16, stride int, output []int16) bool {
	if !pixel.Enabled || stride < 8 || len(input) < 7*stride+8 || len(output) < 64 {
		return false
	}
	in0 := archsimd.LoadInt16x8Array((*[8]int16)(input[0*stride : 0*stride+8]))
	in1 := archsimd.LoadInt16x8Array((*[8]int16)(input[1*stride : 1*stride+8]))
	in2 := archsimd.LoadInt16x8Array((*[8]int16)(input[2*stride : 2*stride+8]))
	in3 := archsimd.LoadInt16x8Array((*[8]int16)(input[3*stride : 3*stride+8]))
	in4 := archsimd.LoadInt16x8Array((*[8]int16)(input[4*stride : 4*stride+8]))
	in5 := archsimd.LoadInt16x8Array((*[8]int16)(input[5*stride : 5*stride+8]))
	in6 := archsimd.LoadInt16x8Array((*[8]int16)(input[6*stride : 6*stride+8]))
	in7 := archsimd.LoadInt16x8Array((*[8]int16)(input[7*stride : 7*stride+8]))

	mag := in0.Abs().ToBits().Max(in1.Abs().ToBits()).Max(in2.Abs().ToBits()).Max(in3.Abs().ToBits()).
		Max(in4.Abs().ToBits()).Max(in5.Abs().ToBits()).Max(in6.Abs().ToBits()).Max(in7.Abs().ToBits())
	if mag.ReduceMax() > 255 {
		return false
	}

	// Pass 1 (columns): |in| <= 255 => |in<<2| <= 1020 => all butterflies fit int16.
	in0 = in0.ShiftAllLeft(2)
	in1 = in1.ShiftAllLeft(2)
	in2 = in2.ShiftAllLeft(2)
	in3 = in3.ShiftAllLeft(2)
	in4 = in4.ShiftAllLeft(2)
	in5 = in5.ShiftAllLeft(2)
	in6 = in6.ShiftAllLeft(2)
	in7 = in7.ShiftAllLeft(2)

	s0 := in0.Add(in7)
	s1 := in1.Add(in6)
	s2 := in2.Add(in5)
	s3 := in3.Add(in4)
	s4 := in3.Sub(in4)
	s5 := in2.Sub(in5)
	s6 := in1.Sub(in6)
	s7 := in0.Sub(in7)

	x0 := s0.Add(s3)
	x1 := s1.Add(s2)
	x2 := s1.Sub(s2)
	x3 := s0.Sub(s3)

	out0 := fdctMul1Round16(x0.Add(x1), arm64Cospi16_16)
	out2 := fdctMul2Round16(x2, arm64Cospi24_16, x3, arm64Cospi8_16)
	out4 := fdctMul1Round16(x0.Sub(x1), arm64Cospi16_16)
	out6 := fdctMul2Round16(x2, arm64NegCospi8_16, x3, arm64Cospi24_16)

	t2 := fdctMul1Round16(s6.Sub(s5), arm64Cospi16_16)
	t3 := fdctMul1Round16(s6.Add(s5), arm64Cospi16_16)
	x0 = s4.Add(t2)
	x1 = s4.Sub(t2)
	x2 = s7.Sub(t3)
	x3 = s7.Add(t3)

	out1 := fdctMul2Round16(x0, arm64Cospi28_16, x3, arm64Cospi4_16)
	out3 := fdctMul2Round16(x2, arm64Cospi12_16, x1, arm64NegCospi20_16)
	out5 := fdctMul2Round16(x1, arm64Cospi12_16, x2, arm64Cospi20_16)
	out7 := fdctMul2Round16(x3, arm64Cospi28_16, x0, arm64NegCospi4_16)

	// In-register 8x8 transpose between column and row passes.
	in0, in1, in2, in3, in4, in5, in6, in7 = transpose8x8Int16(out0, out1, out2, out3, out4, out5, out6, out7)

	// Pass 2 (rows): |in| <= 8160. s0..s7 <= 16320, x0..x3 <= 32640 (fits int16).
	// Widen x0±x1 and odd stage (s4±t2, s7±t3) in Int32x4 to avoid int16 overflow.
	s0 = in0.Add(in7)
	s1 = in1.Add(in6)
	s2 = in2.Add(in5)
	s3 = in3.Add(in4)
	s4 = in3.Sub(in4)
	s5 = in2.Sub(in5)
	s6 = in1.Sub(in6)
	s7 = in0.Sub(in7)

	x0 = s0.Add(s3)
	x1 = s1.Add(s2)
	x2 = s1.Sub(s2)
	x3 = s0.Sub(s3)

	o0Lo := pixel.RoundTransform4(x0.MulWidenLo(arm64Cospi16_16).Add(x1.MulWidenLo(arm64Cospi16_16)))
	o0Hi := pixel.RoundTransform4(x0.HiToLo().MulWidenLo(arm64Cospi16_16).Add(x1.HiToLo().MulWidenLo(arm64Cospi16_16)))
	out0 = fdctDiv2Pack(o0Lo, o0Hi)

	o2Lo := pixel.RoundTransform4(x2.MulWidenLo(arm64Cospi24_16).Add(x3.MulWidenLo(arm64Cospi8_16)))
	o2Hi := pixel.RoundTransform4(x2.HiToLo().MulWidenLo(arm64Cospi24_16).Add(x3.HiToLo().MulWidenLo(arm64Cospi8_16)))
	out2 = fdctDiv2Pack(o2Lo, o2Hi)

	o4Lo := pixel.RoundTransform4(x0.MulWidenLo(arm64Cospi16_16).Sub(x1.MulWidenLo(arm64Cospi16_16)))
	o4Hi := pixel.RoundTransform4(x0.HiToLo().MulWidenLo(arm64Cospi16_16).Sub(x1.HiToLo().MulWidenLo(arm64Cospi16_16)))
	out4 = fdctDiv2Pack(o4Lo, o4Hi)

	o6Lo := pixel.RoundTransform4(x3.MulWidenLo(arm64Cospi24_16).Sub(x2.MulWidenLo(arm64Cospi8_16)))
	o6Hi := pixel.RoundTransform4(x3.HiToLo().MulWidenLo(arm64Cospi24_16).Sub(x2.HiToLo().MulWidenLo(arm64Cospi8_16)))
	out6 = fdctDiv2Pack(o6Lo, o6Hi)

	t2Lo := pixel.RoundTransform4(s6.MulWidenLo(arm64Cospi16_16).Sub(s5.MulWidenLo(arm64Cospi16_16)))
	t2Hi := pixel.RoundTransform4(s6.HiToLo().MulWidenLo(arm64Cospi16_16).Sub(s5.HiToLo().MulWidenLo(arm64Cospi16_16)))
	t3Lo := pixel.RoundTransform4(s6.MulWidenLo(arm64Cospi16_16).Add(s5.MulWidenLo(arm64Cospi16_16)))
	t3Hi := pixel.RoundTransform4(s6.HiToLo().MulWidenLo(arm64Cospi16_16).Add(s5.HiToLo().MulWidenLo(arm64Cospi16_16)))

	s4Lo, s4Hi := s4.ExtendLo4ToInt32(), s4.HiToLo().ExtendLo4ToInt32()
	s7Lo, s7Hi := s7.ExtendLo4ToInt32(), s7.HiToLo().ExtendLo4ToInt32()

	ox0Lo, ox0Hi := s4Lo.Add(t2Lo), s4Hi.Add(t2Hi)
	ox1Lo, ox1Hi := s4Lo.Sub(t2Lo), s4Hi.Sub(t2Hi)
	ox2Lo, ox2Hi := s7Lo.Sub(t3Lo), s7Hi.Sub(t3Hi)
	ox3Lo, ox3Hi := s7Lo.Add(t3Lo), s7Hi.Add(t3Hi)

	o1Lo := pixel.RoundTransform4(pixel.MulAdd4(ox0Lo, arm64Cospi28_32, ox3Lo.Mul(arm64Cospi4_32)))
	o1Hi := pixel.RoundTransform4(pixel.MulAdd4(ox0Hi, arm64Cospi28_32, ox3Hi.Mul(arm64Cospi4_32)))
	out1 = fdctDiv2Pack(o1Lo, o1Hi)

	o3Lo := pixel.RoundTransform4(pixel.MulAdd4(ox2Lo, arm64Cospi12_32, ox1Lo.Mul(arm64NegCospi20_32)))
	o3Hi := pixel.RoundTransform4(pixel.MulAdd4(ox2Hi, arm64Cospi12_32, ox1Hi.Mul(arm64NegCospi20_32)))
	out3 = fdctDiv2Pack(o3Lo, o3Hi)

	o5Lo := pixel.RoundTransform4(pixel.MulAdd4(ox1Lo, arm64Cospi12_32, ox2Lo.Mul(arm64Cospi20_32)))
	o5Hi := pixel.RoundTransform4(pixel.MulAdd4(ox1Hi, arm64Cospi12_32, ox2Hi.Mul(arm64Cospi20_32)))
	out5 = fdctDiv2Pack(o5Lo, o5Hi)

	o7Lo := pixel.RoundTransform4(pixel.MulAdd4(ox3Lo, arm64Cospi28_32, ox0Lo.Mul(arm64NegCospi4_32)))
	o7Hi := pixel.RoundTransform4(pixel.MulAdd4(ox3Hi, arm64Cospi28_32, ox0Hi.Mul(arm64NegCospi4_32)))
	out7 = fdctDiv2Pack(o7Lo, o7Hi)

	// Final in-register 8x8 transpose and 8 SIMD stores.
	out0, out1, out2, out3, out4, out5, out6, out7 = transpose8x8Int16(out0, out1, out2, out3, out4, out5, out6, out7)
	out0.StoreArray((*[8]int16)(output[0:8]))
	out1.StoreArray((*[8]int16)(output[8:16]))
	out2.StoreArray((*[8]int16)(output[16:24]))
	out3.StoreArray((*[8]int16)(output[24:32]))
	out4.StoreArray((*[8]int16)(output[32:40]))
	out5.StoreArray((*[8]int16)(output[40:48]))
	out6.StoreArray((*[8]int16)(output[48:56]))
	out7.StoreArray((*[8]int16)(output[56:64]))
	return true
}

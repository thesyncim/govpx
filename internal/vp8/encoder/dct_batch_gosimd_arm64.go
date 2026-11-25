//go:build goexperiment.simd && arm64 && !purego

// Two-block row/column dataflow follows libvpx v1.16.0 vp8_short_fdct4x4_c.
package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

var (
	vp8BatchK2217    = archsimd.BroadcastInt16x8(2217)
	vp8BatchK5352    = archsimd.BroadcastInt16x8(5352)
	vp8BatchKNeg5352 = archsimd.BroadcastInt16x8(-5352)
	vp8BatchSeven    = archsimd.BroadcastInt16x8(7)
	vp8BatchOne      = archsimd.BroadcastInt16x8(1)
	vp8BatchB14500   = archsimd.BroadcastInt32x4(14500)
	vp8BatchB7500    = archsimd.BroadcastInt32x4(7500)
	vp8BatchB12000   = archsimd.BroadcastInt32x4(12000)
	vp8BatchB51000   = archsimd.BroadcastInt32x4(51000)
)

func forwardDCT4BatchGo(input, output []int16, count int) int {
	if count < 2 || !pixel.Enabled {
		return 0
	}
	_ = input[count*16-1]
	_ = output[count*16-1]
	if count > len(input)/16 || count > len(output)/16 {
		return 0
	}
	k2217, k5352, kNeg5352 := vp8BatchK2217, vp8BatchK5352, vp8BatchKNeg5352
	seven, one := vp8BatchSeven, vp8BatchOne
	b14500, b7500 := vp8BatchB14500, vp8BatchB7500
	b12000, b51000 := vp8BatchB12000, vp8BatchB51000
	var zero16 archsimd.Int16x8
	i := 0
	for ; i+2 <= count; i += 2 {
		in, out := input[i*16:i*16+32], output[i*16:i*16+32]
		r0, r1, r2, r3 := archsimd.LoadInt16x8Array((*[8]int16)(in[0:8])), archsimd.LoadInt16x8Array((*[8]int16)(in[8:16])), archsimd.LoadInt16x8Array((*[8]int16)(in[16:24])), archsimd.LoadInt16x8Array((*[8]int16)(in[24:32]))
		magnitude := r0.Abs().ToBits().Max(r1.Abs().ToBits()).Max(r2.Abs().ToBits()).Max(r3.Abs().ToBits())
		peak := uint32(magnitude.ReduceMax())
		// One unsigned range check admits only nonzero residual packets.
		// Zero and out-of-range packets share the uncommon branch.
		if peak == 0 || peak > 255 {
			if peak == 0 {
				// libvpx v1.16.0 rounding leaves raster coefficient 1 equal to 1.
				v := archsimd.Int16x8{}.SetElem(1, 1)
				v.StoreArray((*[8]int16)(out[0:8]))
				zero16.StoreArray((*[8]int16)(out[8:16]))
				v.StoreArray((*[8]int16)(out[16:24]))
				zero16.StoreArray((*[8]int16)(out[24:32]))
			} else {
				forwardDCT4x4Scalar(in, 4, (*[16]int16)(out))
				forwardDCT4x4Scalar(in[16:], 4, (*[16]int16)(out[16:]))
			}
			continue
		}
		// Deinterleave the four input columns across eight rows (two blocks).
		even0, odd0 := r0.ConcatEven(r1), r0.ConcatOdd(r1)
		even1, odd1 := r2.ConcatEven(r3), r2.ConcatOdd(r3)
		a, c := even0.ConcatEven(even1), even0.ConcatOdd(even1)
		b, d := odd0.ConcatEven(odd1), odd0.ConcatOdd(odd1)
		x, y := a.Add(d).ShiftAllLeft(3), b.Add(c).ShiftAllLeft(3)
		z, w := b.Sub(c).ShiftAllLeft(3), a.Sub(d).ShiftAllLeft(3)
		v0, v2 := x.Add(y), x.Sub(y)
		v1lo := z.MulWidenLo(k2217).Add(w.MulWidenLo(k5352)).Add(b14500).ShiftAllRight(12)
		v1hi := z.HiToLo().MulWidenLo(k2217).Add(w.HiToLo().MulWidenLo(k5352)).Add(b14500).ShiftAllRight(12)
		v1 := v1lo.ToBits().ReshapeToUint16s().ConcatEven(v1hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		v3lo := w.MulWidenLo(k2217).Add(z.MulWidenLo(kNeg5352)).Add(b7500).ShiftAllRight(12)
		v3hi := w.HiToLo().MulWidenLo(k2217).Add(z.HiToLo().MulWidenLo(kNeg5352)).Add(b7500).ShiftAllRight(12)
		v3 := v3lo.ToBits().ReshapeToUint16s().ConcatEven(v3hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		// Transpose each block independently, retaining all eight lanes.
		t0, t1 := v0.InterleaveLo(v1).ToBits().ReshapeToUint32s(), v2.InterleaveLo(v3).ToBits().ReshapeToUint32s()
		t2, t3 := v0.InterleaveHi(v1).ToBits().ReshapeToUint32s(), v2.InterleaveHi(v3).ToBits().ReshapeToUint32s()
		u0, u1 := t0.InterleaveLo(t1).ReshapeToUint64s(), t0.InterleaveHi(t1).ReshapeToUint64s()
		u2, u3 := t2.InterleaveLo(t3).ReshapeToUint64s(), t2.InterleaveHi(t3).ReshapeToUint64s()
		a, b = u0.InterleaveLo(u2).ReshapeToUint16s().BitsToInt16(), u0.InterleaveHi(u2).ReshapeToUint16s().BitsToInt16()
		c, d = u1.InterleaveLo(u3).ReshapeToUint16s().BitsToInt16(), u1.InterleaveHi(u3).ReshapeToUint16s().BitsToInt16()
		// For residuals within +/-255 the first pass is bounded by 8160;
		// each second-pass int16 sum plus rounding is at most 32647.
		x, y, z, w = a.Add(d), b.Add(c), b.Sub(c), a.Sub(d)
		v0, v2 = x.Add(y).Add(seven).ShiftAllRight(4), x.Sub(y).Add(seven).ShiftAllRight(4)
		o1lo := z.MulWidenLo(k2217).Add(w.MulWidenLo(k5352)).Add(b12000).ShiftAllRight(16)
		o1hi := z.HiToLo().MulWidenLo(k2217).Add(w.HiToLo().MulWidenLo(k5352)).Add(b12000).ShiftAllRight(16)
		o1 := o1lo.ToBits().ReshapeToUint16s().ConcatEven(o1hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		o3lo := w.MulWidenLo(k2217).Add(z.MulWidenLo(kNeg5352)).Add(b51000).ShiftAllRight(16)
		o3hi := w.HiToLo().MulWidenLo(k2217).Add(z.HiToLo().MulWidenLo(kNeg5352)).Add(b51000).ShiftAllRight(16)
		o3 := o3lo.ToBits().ReshapeToUint16s().ConcatEven(o3hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		v1 = o1.Add(w.NotEqual(zero16).ToInt16x8().And(one))
		v3 = o3
		a0, a1, a2, a3 := v0.ToBits().ReshapeToUint64s(), v1.ToBits().ReshapeToUint64s(), v2.ToBits().ReshapeToUint64s(), v3.ToBits().ReshapeToUint64s()
		a0.InterleaveLo(a1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[0:8]))
		a2.InterleaveLo(a3).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[8:16]))
		a0.InterleaveHi(a1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[16:24]))
		a2.InterleaveHi(a3).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[24:32]))
	}
	return i
}

// forwardDCT4BatchResidualsGo transforms pairs of 4x4 blocks guaranteed to lie
// in [-255, 255] without the 4x Abs + 3x Max + ReduceMax overflow scan.
func forwardDCT4BatchResidualsGo(input, output []int16, count int) int {
	if count < 2 || !pixel.Enabled {
		return 0
	}
	if count > len(input)/16 || count > len(output)/16 {
		return 0
	}
	_ = input[count*16-1]
	_ = output[count*16-1]
	k2217, k5352, kNeg5352 := vp8BatchK2217, vp8BatchK5352, vp8BatchKNeg5352
	seven, one := vp8BatchSeven, vp8BatchOne
	b14500, b7500 := vp8BatchB14500, vp8BatchB7500
	b12000, b51000 := vp8BatchB12000, vp8BatchB51000
	var zero16 archsimd.Int16x8
	i := 0
	for ; i+2 <= count; i += 2 {
		in, out := input[i*16:i*16+32], output[i*16:i*16+32]
		r0 := archsimd.LoadInt16x8Array((*[8]int16)(in[0:8]))
		r1 := archsimd.LoadInt16x8Array((*[8]int16)(in[8:16]))
		r2 := archsimd.LoadInt16x8Array((*[8]int16)(in[16:24]))
		r3 := archsimd.LoadInt16x8Array((*[8]int16)(in[24:32]))
		even0, odd0 := r0.ConcatEven(r1), r0.ConcatOdd(r1)
		even1, odd1 := r2.ConcatEven(r3), r2.ConcatOdd(r3)
		a, c := even0.ConcatEven(even1), even0.ConcatOdd(even1)
		b, d := odd0.ConcatEven(odd1), odd0.ConcatOdd(odd1)
		x, y := a.Add(d).ShiftAllLeft(3), b.Add(c).ShiftAllLeft(3)
		z, w := b.Sub(c).ShiftAllLeft(3), a.Sub(d).ShiftAllLeft(3)
		v0, v2 := x.Add(y), x.Sub(y)
		zh, wh := z.HiToLo(), w.HiToLo()
		v1lo := z.MulWidenLo(k2217).Add(w.MulWidenLo(k5352)).Add(b14500).ShiftAllRight(12)
		v1hi := zh.MulWidenLo(k2217).Add(wh.MulWidenLo(k5352)).Add(b14500).ShiftAllRight(12)
		v1 := v1lo.ToBits().ReshapeToUint16s().ConcatEven(v1hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		v3lo := w.MulWidenLo(k2217).Add(z.MulWidenLo(kNeg5352)).Add(b7500).ShiftAllRight(12)
		v3hi := wh.MulWidenLo(k2217).Add(zh.MulWidenLo(kNeg5352)).Add(b7500).ShiftAllRight(12)
		v3 := v3lo.ToBits().ReshapeToUint16s().ConcatEven(v3hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		t0, t1 := v0.InterleaveLo(v1).ToBits().ReshapeToUint32s(), v2.InterleaveLo(v3).ToBits().ReshapeToUint32s()
		t2, t3 := v0.InterleaveHi(v1).ToBits().ReshapeToUint32s(), v2.InterleaveHi(v3).ToBits().ReshapeToUint32s()
		u0, u1 := t0.InterleaveLo(t1).ReshapeToUint64s(), t0.InterleaveHi(t1).ReshapeToUint64s()
		u2, u3 := t2.InterleaveLo(t3).ReshapeToUint64s(), t2.InterleaveHi(t3).ReshapeToUint64s()
		a, b = u0.InterleaveLo(u2).ReshapeToUint16s().BitsToInt16(), u0.InterleaveHi(u2).ReshapeToUint16s().BitsToInt16()
		c, d = u1.InterleaveLo(u3).ReshapeToUint16s().BitsToInt16(), u1.InterleaveHi(u3).ReshapeToUint16s().BitsToInt16()
		x, y, z, w = a.Add(d), b.Add(c), b.Sub(c), a.Sub(d)
		v0, v2 = x.Add(y).Add(seven).ShiftAllRight(4), x.Sub(y).Add(seven).ShiftAllRight(4)
		zh, wh = z.HiToLo(), w.HiToLo()
		o1lo := z.MulWidenLo(k2217).Add(w.MulWidenLo(k5352)).Add(b12000).ShiftAllRight(16)
		o1hi := zh.MulWidenLo(k2217).Add(wh.MulWidenLo(k5352)).Add(b12000).ShiftAllRight(16)
		o1 := o1lo.ToBits().ReshapeToUint16s().ConcatEven(o1hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		o3lo := w.MulWidenLo(k2217).Add(z.MulWidenLo(kNeg5352)).Add(b51000).ShiftAllRight(16)
		o3hi := wh.MulWidenLo(k2217).Add(zh.MulWidenLo(kNeg5352)).Add(b51000).ShiftAllRight(16)
		o3 := o3lo.ToBits().ReshapeToUint16s().ConcatEven(o3hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		v1 = o1.Add(w.NotEqual(zero16).ToInt16x8().And(one))
		v3 = o3
		a0, a1, a2, a3 := v0.ToBits().ReshapeToUint64s(), v1.ToBits().ReshapeToUint64s(), v2.ToBits().ReshapeToUint64s(), v3.ToBits().ReshapeToUint64s()
		a0.InterleaveLo(a1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[0:8]))
		a2.InterleaveLo(a3).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[8:16]))
		a0.InterleaveHi(a1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[16:24]))
		a2.InterleaveHi(a3).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[24:32]))
	}
	return i
}

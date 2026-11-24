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
				z := archsimd.Int16x8{}
				v.StoreArray((*[8]int16)(out[0:8]))
				z.StoreArray((*[8]int16)(out[8:16]))
				v.StoreArray((*[8]int16)(out[16:24]))
				z.StoreArray((*[8]int16)(out[24:32]))
			} else {
				forwardDCT4x4Scalar(in, 4, (*[16]int16)(out))
				forwardDCT4x4Scalar(in[16:], 4, (*[16]int16)(out[16:]))
			}
			continue
		}
		// Constant blocks have one DC term and the exact raster-1 rounding term.
		dc0, dc1 := r0.GetElem(0), r2.GetElem(0)
		if dc0 == r1.GetElem(7) && dc1 == r3.GetElem(7) {
			same0, same1 := archsimd.BroadcastInt16x8(dc0), archsimd.BroadcastInt16x8(dc1)
			diff := r0.Xor(same0).Or(r1.Xor(same0)).Or(r2.Xor(same1)).Or(r3.Xor(same1))
			if diff.ToBits().ReduceMax() == 0 {
				v0 := archsimd.Int16x8{}.SetElem(0, dc0*8).SetElem(1, 1)
				v1 := archsimd.Int16x8{}.SetElem(0, dc1*8).SetElem(1, 1)
				z := archsimd.Int16x8{}
				v0.StoreArray((*[8]int16)(out[0:8]))
				z.StoreArray((*[8]int16)(out[8:16]))
				v1.StoreArray((*[8]int16)(out[16:24]))
				z.StoreArray((*[8]int16)(out[24:32]))
				continue
			}

		}
		// Deinterleave the four input columns across eight rows (two blocks).
		even0, odd0 := r0.ConcatEven(r1), r0.ConcatOdd(r1)
		even1, odd1 := r2.ConcatEven(r3), r2.ConcatOdd(r3)
		a, c := even0.ConcatEven(even1), even0.ConcatOdd(even1)
		b, d := odd0.ConcatEven(odd1), odd0.ConcatOdd(odd1)
		x, y := a.Add(d).ShiftAllLeft(3), b.Add(c).ShiftAllLeft(3)
		z, w := b.Sub(c).ShiftAllLeft(3), a.Sub(d).ShiftAllLeft(3)
		v0, v2 := x.Add(y), x.Sub(y)
		k2217, k5352, kNeg5352 := vp8BatchK2217, vp8BatchK5352, vp8BatchKNeg5352
		v1lo := vp8Right12(z.MulWidenLo(k2217).Add(w.MulWidenLo(k5352)).Add(vp8BatchB14500))
		v1hi := vp8Right12(z.HiToLo().MulWidenLo(k2217).Add(w.HiToLo().MulWidenLo(k5352)).Add(vp8BatchB14500))
		v1 := v1lo.ToBits().ReshapeToUint16s().ConcatEven(v1hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		v3lo := vp8Right12(w.MulWidenLo(k2217).Add(z.MulWidenLo(kNeg5352)).Add(vp8BatchB7500))
		v3hi := vp8Right12(w.HiToLo().MulWidenLo(k2217).Add(z.HiToLo().MulWidenLo(kNeg5352)).Add(vp8BatchB7500))
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
		v0, v2 = x.Add(y).Add(vp8BatchSeven).ShiftAllRight(4), x.Sub(y).Add(vp8BatchSeven).ShiftAllRight(4)
		o1lo := vp8Right16(z.MulWidenLo(k2217).Add(w.MulWidenLo(k5352)).Add(vp8BatchB12000))
		o1hi := vp8Right16(z.HiToLo().MulWidenLo(k2217).Add(w.HiToLo().MulWidenLo(k5352)).Add(vp8BatchB12000))
		o1 := o1lo.ToBits().ReshapeToUint16s().ConcatEven(o1hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		o3lo := vp8Right16(w.MulWidenLo(k2217).Add(z.MulWidenLo(kNeg5352)).Add(vp8BatchB51000))
		o3hi := vp8Right16(w.HiToLo().MulWidenLo(k2217).Add(z.HiToLo().MulWidenLo(kNeg5352)).Add(vp8BatchB51000))
		o3 := o3lo.ToBits().ReshapeToUint16s().ConcatEven(o3hi.ToBits().ReshapeToUint16s()).BitsToInt16()
		v1 = o1.Add(w.NotEqual(archsimd.Int16x8{}).ToInt16x8().And(vp8BatchOne))
		v3 = o3
		a0, a1, a2, a3 := v0.ToBits().ReshapeToUint64s(), v1.ToBits().ReshapeToUint64s(), v2.ToBits().ReshapeToUint64s(), v3.ToBits().ReshapeToUint64s()
		a0.InterleaveLo(a1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[0:8]))
		a2.InterleaveLo(a3).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[8:16]))
		a0.InterleaveHi(a1).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[16:24]))
		a2.InterleaveHi(a3).ReshapeToUint16s().BitsToInt16().StoreArray((*[8]int16)(out[24:32]))
	}
	return i
}

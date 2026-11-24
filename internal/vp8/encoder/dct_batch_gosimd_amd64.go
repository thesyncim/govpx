//go:build goexperiment.simd && amd64 && !purego

// Two-block row/column dataflow follows libvpx v1.16.0 vp8_short_fdct4x4_c.
package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

var vp8BatchEvenIndex = [16]int8{0, 1, 4, 5, 8, 9, 12, 13, -128, -128, -128, -128, -128, -128, -128, -128}
var vp8BatchOddIndex = [16]int8{2, 3, 6, 7, 10, 11, 14, 15, -128, -128, -128, -128, -128, -128, -128, -128}
var vp8BatchK1 = [8]int16{2217, 5352, 2217, 5352, 2217, 5352, 2217, 5352}
var vp8BatchK3 = [8]int16{-5352, 2217, -5352, 2217, -5352, 2217, -5352, 2217}

func vp8BatchUnzip(a, b archsimd.Int16x8, index *[16]int8) archsimd.Int16x8 {
	indices := archsimd.LoadInt8x16Array(index)
	x, y := a.ToBits().ReshapeToUint8s().PermuteOrZero(indices).ReshapeToUint64s(), b.ToBits().ReshapeToUint8s().PermuteOrZero(indices).ReshapeToUint64s()
	return x.InterleaveLo(y).ReshapeToUint16s().BitsToInt16()
}

func forwardDCT4BatchResidualsGo(input, output []int16, count int) int {
	return forwardDCT4BatchGo(input, output, count)
}

func forwardDCT4BatchGo(input, output []int16, count int) int {
	if count < 2 || !pixel.Enabled {
		return 0
	}
	_ = input[count*16-1]
	_ = output[count*16-1]
	i := 0
	for ; i+2 <= count; i += 2 {
		in, out := input[i*16:], output[i*16:]
		r0, r1, r2, r3 := archsimd.LoadInt16x8(in), archsimd.LoadInt16x8(in[8:]), archsimd.LoadInt16x8(in[16:]), archsimd.LoadInt16x8(in[24:])
		magnitude := r0.Abs().ToBits().Max(r1.Abs().ToBits()).Max(r2.Abs().ToBits()).Max(r3.Abs().ToBits())
		peak := (magnitude.ReshapeToUint64s().GetElem(0) | magnitude.ReshapeToUint64s().GetElem(1))
		if peak == 0 {
			// The pinned libvpx integer transform maps zero residual to
			// coefficient 1 at raster index 1 because of its rounding biases.
			v := archsimd.Int16x8{}.SetElem(1, 1)
			z := archsimd.Int16x8{}
			v.Store(out)
			z.Store(out[8:])
			v.Store(out[16:])
			z.Store(out[24:])
			continue
		}
		if peak&0xff00ff00ff00ff00 != 0 {
			forwardDCT4x4Scalar(in, 4, (*[16]int16)(out))
			forwardDCT4x4Scalar(in[16:], 4, (*[16]int16)(out[16:]))
			continue
		}
		// Deinterleave the four input columns across eight rows (two blocks).
		even0, odd0 := vp8BatchUnzip(r0, r1, &vp8BatchEvenIndex), vp8BatchUnzip(r0, r1, &vp8BatchOddIndex)
		even1, odd1 := vp8BatchUnzip(r2, r3, &vp8BatchEvenIndex), vp8BatchUnzip(r2, r3, &vp8BatchOddIndex)
		a, c := vp8BatchUnzip(even0, even1, &vp8BatchEvenIndex), vp8BatchUnzip(even0, even1, &vp8BatchOddIndex)
		b, d := vp8BatchUnzip(odd0, odd1, &vp8BatchEvenIndex), vp8BatchUnzip(odd0, odd1, &vp8BatchOddIndex)
		x, y := a.Add(d).ShiftAllLeft(3), b.Add(c).ShiftAllLeft(3)
		z, w := b.Sub(c).ShiftAllLeft(3), a.Sub(d).ShiftAllLeft(3)
		v0, v2 := x.Add(y), x.Sub(y)
		k1, k3 := archsimd.LoadInt16x8Array(&vp8BatchK1), archsimd.LoadInt16x8Array(&vp8BatchK3)
		v1 := vp8Right12(z.InterleaveLo(w).DotProductPairs(k1).Add(archsimd.BroadcastInt32x4(14500))).SaturateToInt16Concat(vp8Right12(z.InterleaveHi(w).DotProductPairs(k1).Add(archsimd.BroadcastInt32x4(14500))))
		v3 := vp8Right12(z.InterleaveLo(w).DotProductPairs(k3).Add(archsimd.BroadcastInt32x4(7500))).SaturateToInt16Concat(vp8Right12(z.InterleaveHi(w).DotProductPairs(k3).Add(archsimd.BroadcastInt32x4(7500))))
		// Transpose each block independently, retaining all eight lanes.
		t0, t1 := v0.InterleaveLo(v1).ToBits().ReshapeToUint32s(), v2.InterleaveLo(v3).ToBits().ReshapeToUint32s()
		t2, t3 := v0.InterleaveHi(v1).ToBits().ReshapeToUint32s(), v2.InterleaveHi(v3).ToBits().ReshapeToUint32s()
		u0, u1 := t0.InterleaveLo(t1).ReshapeToUint64s(), t0.InterleaveHi(t1).ReshapeToUint64s()
		u2, u3 := t2.InterleaveLo(t3).ReshapeToUint64s(), t2.InterleaveHi(t3).ReshapeToUint64s()
		a, b = u0.InterleaveLo(u2).ReshapeToUint16s().BitsToInt16(), u0.InterleaveHi(u2).ReshapeToUint16s().BitsToInt16()
		c, d = u1.InterleaveLo(u3).ReshapeToUint16s().BitsToInt16(), u1.InterleaveHi(u3).ReshapeToUint16s().BitsToInt16()
		// For residuals within +/-255 the first pass is bounded by8160;
		// each second-pass int16 sum plus rounding is at most32647.
		x, y, z, w = a.Add(d), b.Add(c), b.Sub(c), a.Sub(d)
		seven := archsimd.BroadcastInt16x8(7)
		v0, v2 = x.Add(y).Add(seven).ShiftAllRight(4), x.Sub(y).Add(seven).ShiftAllRight(4)
		o1 := vp8Right16(z.InterleaveLo(w).DotProductPairs(k1).Add(archsimd.BroadcastInt32x4(12000))).SaturateToInt16Concat(vp8Right16(z.InterleaveHi(w).DotProductPairs(k1).Add(archsimd.BroadcastInt32x4(12000))))
		o3 := vp8Right16(z.InterleaveLo(w).DotProductPairs(k3).Add(archsimd.BroadcastInt32x4(51000))).SaturateToInt16Concat(vp8Right16(z.InterleaveHi(w).DotProductPairs(k3).Add(archsimd.BroadcastInt32x4(51000))))
		v1 = o1.Add(w.NotEqual(archsimd.Int16x8{}).ToInt16x8().And(archsimd.BroadcastInt16x8(1)))
		v3 = o3
		a0, a1, a2, a3 := v0.ToBits().ReshapeToUint64s(), v1.ToBits().ReshapeToUint64s(), v2.ToBits().ReshapeToUint64s(), v3.ToBits().ReshapeToUint64s()
		a0.InterleaveLo(a1).ReshapeToUint16s().BitsToInt16().Store(out)
		a2.InterleaveLo(a3).ReshapeToUint16s().BitsToInt16().Store(out[8:])
		a0.InterleaveHi(a1).ReshapeToUint16s().BitsToInt16().Store(out[16:])
		a2.InterleaveHi(a3).ReshapeToUint16s().BitsToInt16().Store(out[24:])
	}
	return i
}

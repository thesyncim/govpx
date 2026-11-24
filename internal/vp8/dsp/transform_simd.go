//go:build goexperiment.simd && (arm64 || amd64) && !purego

package dsp

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

// Arithmetic follows libvpx v1.16.0 vp8/common/idctllm.c.
func inverse4Go(input *[16]int16, pred []byte, ps int, dst []byte, ds int, walsh bool, mb []int16) bool {
	if !pixel.Enabled {
		return false
	}
	in0, in1, in2, in3 := pixel.Load4(input[:]), pixel.Load4(input[4:]), pixel.Load4(input[8:]), pixel.Load4(input[12:])
	var out0, out1, out2, out3 archsimd.Int32x4
	{
		if walsh {
			a, b, c, d := in0.Add(in3), in1.Add(in2), in1.Sub(in2), in0.Sub(in3)
			out0, out1, out2, out3 = a.Add(b), c.Add(d), a.Sub(b), d.Sub(c)
		} else {
			a, b := in0.Add(in2), in0.Sub(in2)
			c := in1.Mul(archsimd.BroadcastInt32x4(sinPI8Sqrt2)).ShiftAllRight(16).Sub(in3.Add(in3.Mul(archsimd.BroadcastInt32x4(cosPI8Sqrt2Minus1)).ShiftAllRight(16)))
			d := in1.Add(in1.Mul(archsimd.BroadcastInt32x4(cosPI8Sqrt2Minus1)).ShiftAllRight(16)).Add(in3.Mul(archsimd.BroadcastInt32x4(sinPI8Sqrt2)).ShiftAllRight(16))
			out0, out1, out2, out3 = a.Add(d), b.Add(c), b.Sub(c), a.Sub(d)
		}
		out0 = out0.ShiftAllLeft(16).ShiftAllRight(16)
		out1 = out1.ShiftAllLeft(16).ShiftAllRight(16)
		out2 = out2.ShiftAllLeft(16).ShiftAllRight(16)
		out3 = out3.ShiftAllLeft(16).ShiftAllRight(16)
	}
	in0, in1, in2, in3 = pixel.Transpose4(out0, out1, out2, out3)
	{
		if walsh {
			a, b, c, d := in0.Add(in3), in1.Add(in2), in1.Sub(in2), in0.Sub(in3)
			out0, out1, out2, out3 = a.Add(b), c.Add(d), a.Sub(b), d.Sub(c)
		} else {
			a, b := in0.Add(in2), in0.Sub(in2)
			c := in1.Mul(archsimd.BroadcastInt32x4(sinPI8Sqrt2)).ShiftAllRight(16).Sub(in3.Add(in3.Mul(archsimd.BroadcastInt32x4(cosPI8Sqrt2Minus1)).ShiftAllRight(16)))
			d := in1.Add(in1.Mul(archsimd.BroadcastInt32x4(cosPI8Sqrt2Minus1)).ShiftAllRight(16)).Add(in3.Mul(archsimd.BroadcastInt32x4(sinPI8Sqrt2)).ShiftAllRight(16))
			out0, out1, out2, out3 = a.Add(d), b.Add(c), b.Sub(c), a.Sub(d)
		}
		bias := int32(4)
		if walsh {
			bias = 3
		}
		out0 = out0.Add(archsimd.BroadcastInt32x4(bias)).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
		out1 = out1.Add(archsimd.BroadcastInt32x4(bias)).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
		out2 = out2.Add(archsimd.BroadcastInt32x4(bias)).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
		out3 = out3.Add(archsimd.BroadcastInt32x4(bias)).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	}
	out0, out1, out2, out3 = pixel.Transpose4(out0, out1, out2, out3)
	if walsh {
		mb[0] = int16(out0.GetElem(0))
		mb[16] = int16(out0.GetElem(1))
		mb[32] = int16(out0.GetElem(2))
		mb[48] = int16(out0.GetElem(3))
		mb[64] = int16(out1.GetElem(0))
		mb[80] = int16(out1.GetElem(1))
		mb[96] = int16(out1.GetElem(2))
		mb[112] = int16(out1.GetElem(3))
		mb[128] = int16(out2.GetElem(0))
		mb[144] = int16(out2.GetElem(1))
		mb[160] = int16(out2.GetElem(2))
		mb[176] = int16(out2.GetElem(3))
		mb[192] = int16(out3.GetElem(0))
		mb[208] = int16(out3.GetElem(1))
		mb[224] = int16(out3.GetElem(2))
		mb[240] = int16(out3.GetElem(3))
	} else {
		pixel.StorePixels4(dst, out0.Add(pixel.LoadPixels4(pred)))
		pixel.StorePixels4(dst[ds:], out1.Add(pixel.LoadPixels4(pred[ps:])))
		pixel.StorePixels4(dst[2*ds:], out2.Add(pixel.LoadPixels4(pred[2*ps:])))
		pixel.StorePixels4(dst[3*ds:], out3.Add(pixel.LoadPixels4(pred[3*ps:])))
	}
	return true
}

// idct4x4Pass runs one 4-row VP8 inverse DCT butterfly on 4 Int32x4 rows.
func idct4x4Pass(in0, in1, in2, in3 archsimd.Int32x4, sinC, cosC archsimd.Int32x4) (archsimd.Int32x4, archsimd.Int32x4, archsimd.Int32x4, archsimd.Int32x4) {
	a, b := in0.Add(in2), in0.Sub(in2)
	c := in1.Mul(sinC).ShiftAllRight(16).Sub(in3.Add(in3.Mul(cosC).ShiftAllRight(16)))
	d := in1.Add(in1.Mul(cosC).ShiftAllRight(16)).Add(in3.Mul(sinC).ShiftAllRight(16))
	return a.Add(d), b.Add(c), b.Sub(c), a.Sub(d)
}

func hi4ToInt32(v archsimd.Int16x8) archsimd.Int32x4 {
	u := v.ToBits().ReshapeToUint64s()
	return u.InterleaveHi(u).ReshapeToUint16s().BitsToInt16().ExtendLo4ToInt32()
}

// dequantInverse4PairGo fuses dequantization + 4x4 IDCT + saturating add for
// two horizontally adjacent 4x4 blocks (8x4 destination) without intermediate
// stack buffers or separate pixel.Mul16 calls.
func dequantInverse4PairGo(q *[32]int16, dq *[16]int16, dst []byte, stride int) bool {
	if !pixel.Enabled || stride < 8 || 3*stride+8 > len(dst) {
		return false
	}
	dq0 := archsimd.LoadInt16x8Array((*[8]int16)(dq[0:8]))
	dq1 := archsimd.LoadInt16x8Array((*[8]int16)(dq[8:16]))
	sinC := archsimd.BroadcastInt32x4(sinPI8Sqrt2)
	cosC := archsimd.BroadcastInt32x4(cosPI8Sqrt2Minus1)
	bias4 := archsimd.BroadcastInt32x4(4)

	// Block 0 (left 4x4)
	p0 := archsimd.LoadInt16x8Array((*[8]int16)(q[0:8])).Mul(dq0)
	p1 := archsimd.LoadInt16x8Array((*[8]int16)(q[8:16])).Mul(dq1)
	in0 := p0.ExtendLo4ToInt32()
	in1 := hi4ToInt32(p0)
	in2 := p1.ExtendLo4ToInt32()
	in3 := hi4ToInt32(p1)

	o0, o1, o2, o3 := idct4x4Pass(in0, in1, in2, in3, sinC, cosC)
	o0 = o0.ShiftAllLeft(16).ShiftAllRight(16)
	o1 = o1.ShiftAllLeft(16).ShiftAllRight(16)
	o2 = o2.ShiftAllLeft(16).ShiftAllRight(16)
	o3 = o3.ShiftAllLeft(16).ShiftAllRight(16)
	in0, in1, in2, in3 = pixel.Transpose4(o0, o1, o2, o3)
	o0, o1, o2, o3 = idct4x4Pass(in0, in1, in2, in3, sinC, cosC)
	o0 = o0.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	o1 = o1.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	o2 = o2.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	o3 = o3.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	l0, l1, l2, l3 := pixel.Transpose4(o0, o1, o2, o3)

	// Block 1 (right 4x4)
	p2 := archsimd.LoadInt16x8Array((*[8]int16)(q[16:24])).Mul(dq0)
	p3 := archsimd.LoadInt16x8Array((*[8]int16)(q[24:32])).Mul(dq1)
	in0 = p2.ExtendLo4ToInt32()
	in1 = hi4ToInt32(p2)
	in2 = p3.ExtendLo4ToInt32()
	in3 = hi4ToInt32(p3)

	o0, o1, o2, o3 = idct4x4Pass(in0, in1, in2, in3, sinC, cosC)
	o0 = o0.ShiftAllLeft(16).ShiftAllRight(16)
	o1 = o1.ShiftAllLeft(16).ShiftAllRight(16)
	o2 = o2.ShiftAllLeft(16).ShiftAllRight(16)
	o3 = o3.ShiftAllLeft(16).ShiftAllRight(16)
	in0, in1, in2, in3 = pixel.Transpose4(o0, o1, o2, o3)
	o0, o1, o2, o3 = idct4x4Pass(in0, in1, in2, in3, sinC, cosC)
	o0 = o0.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	o1 = o1.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	o2 = o2.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	o3 = o3.Add(bias4).ShiftAllRight(3).ShiftAllLeft(16).ShiftAllRight(16)
	r0, r1, r2, r3 := pixel.Transpose4(o0, o1, o2, o3)

	pixel.StorePixels4(dst, l0.Add(pixel.LoadPixels4(dst)))
	pixel.StorePixels4(dst[4:], r0.Add(pixel.LoadPixels4(dst[4:])))
	d1 := dst[stride:]
	pixel.StorePixels4(d1, l1.Add(pixel.LoadPixels4(d1)))
	pixel.StorePixels4(d1[4:], r1.Add(pixel.LoadPixels4(d1[4:])))
	d2 := dst[2*stride:]
	pixel.StorePixels4(d2, l2.Add(pixel.LoadPixels4(d2)))
	pixel.StorePixels4(d2[4:], r2.Add(pixel.LoadPixels4(d2[4:])))
	d3 := dst[3*stride:]
	pixel.StorePixels4(d3, l3.Add(pixel.LoadPixels4(d3)))
	pixel.StorePixels4(d3[4:], r3.Add(pixel.LoadPixels4(d3[4:])))
	return true
}

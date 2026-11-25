//go:build goexperiment.simd && (arm64 || amd64) && !purego

package encoder

import (
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

// hadamardBlockYrdGo mirrors the scoring-only layout of libvpx v1.16.0
// vpx_dsp/arm/hadamard_neon.c. BlockYrd supplies disjoint residual/output
// windows and residuals in [-255,255]. Each 8x8 coefficient is therefore in
// [-16320,16320], so the 16x16 combine's pre-shift sums fit signed 16 bits.
func hadamardBlockYrdGo(src []int16, stride int, coeff []int16, n int) bool {
	if !pixel.Enabled || (n != 8 && n != 16) || stride < n || len(src) < n ||
		stride > (len(src)-n)/(n-1) || len(coeff) < n*n {
		return false
	}
	if n == 8 {
		hadamardBlockYrd8(src, stride, coeff)
		return true
	}
	hadamardBlockYrd8(src, stride, coeff)
	hadamardBlockYrd8(src[8:], stride, coeff[64:])
	hadamardBlockYrd8(src[8*stride:], stride, coeff[128:])
	hadamardBlockYrd8(src[8*stride+8:], stride, coeff[192:])
	for i := 0; i < 64; i += 8 {
		a := archsimd.LoadInt16x8Array((*[8]int16)(coeff[i : i+8]))
		b := archsimd.LoadInt16x8Array((*[8]int16)(coeff[i+64 : i+72]))
		c := archsimd.LoadInt16x8Array((*[8]int16)(coeff[i+128 : i+136]))
		d := archsimd.LoadInt16x8Array((*[8]int16)(coeff[i+192 : i+200]))
		u, v, w, z := a.Add(b).ShiftAllRight(1), a.Sub(b).ShiftAllRight(1), c.Add(d).ShiftAllRight(1), c.Sub(d).ShiftAllRight(1)
		u.Add(w).StoreArray((*[8]int16)(coeff[i : i+8]))
		v.Add(z).StoreArray((*[8]int16)(coeff[i+64 : i+72]))
		u.Sub(w).StoreArray((*[8]int16)(coeff[i+128 : i+136]))
		v.Sub(z).StoreArray((*[8]int16)(coeff[i+192 : i+200]))
	}
	return true
}

// The caller proves all eight complete input rows and the 64 output samples.
// Named vectors keep both butterflies and the single transpose in registers.
func hadamardBlockYrd8(src []int16, stride int, coeff []int16) {
	v0 := archsimd.LoadInt16x8Array((*[8]int16)(src[0*stride : 0*stride+8]))
	v1 := archsimd.LoadInt16x8Array((*[8]int16)(src[1*stride : 1*stride+8]))
	v2 := archsimd.LoadInt16x8Array((*[8]int16)(src[2*stride : 2*stride+8]))
	v3 := archsimd.LoadInt16x8Array((*[8]int16)(src[3*stride : 3*stride+8]))
	v4 := archsimd.LoadInt16x8Array((*[8]int16)(src[4*stride : 4*stride+8]))
	v5 := archsimd.LoadInt16x8Array((*[8]int16)(src[5*stride : 5*stride+8]))
	v6 := archsimd.LoadInt16x8Array((*[8]int16)(src[6*stride : 6*stride+8]))
	v7 := archsimd.LoadInt16x8Array((*[8]int16)(src[7*stride : 7*stride+8]))
	{
		b0, b1 := v0.Add(v1), v0.Sub(v1)
		b2, b3 := v2.Add(v3), v2.Sub(v3)
		b4, b5 := v4.Add(v5), v4.Sub(v5)
		b6, b7 := v6.Add(v7), v6.Sub(v7)
		c0, c1, c2, c3 := b0.Add(b2), b1.Add(b3), b0.Sub(b2), b1.Sub(b3)
		c4, c5, c6, c7 := b4.Add(b6), b5.Add(b7), b4.Sub(b6), b5.Sub(b7)
		v0, v7, v3, v4, v2, v6, v1, v5 = c0.Add(c4), c1.Add(c5), c2.Add(c6), c3.Add(c7), c0.Sub(c4), c1.Sub(c5), c2.Sub(c6), c3.Sub(c7)
	}
	{
		a0, a1 := v0.InterleaveLo(v1).ToBits().ReshapeToUint32s(), v0.InterleaveHi(v1).ToBits().ReshapeToUint32s()
		a2, a3 := v2.InterleaveLo(v3).ToBits().ReshapeToUint32s(), v2.InterleaveHi(v3).ToBits().ReshapeToUint32s()
		a4, a5 := v4.InterleaveLo(v5).ToBits().ReshapeToUint32s(), v4.InterleaveHi(v5).ToBits().ReshapeToUint32s()
		a6, a7 := v6.InterleaveLo(v7).ToBits().ReshapeToUint32s(), v6.InterleaveHi(v7).ToBits().ReshapeToUint32s()
		b0, b1, b2, b3 := a0.InterleaveLo(a2).ReshapeToUint64s(), a0.InterleaveHi(a2).ReshapeToUint64s(), a1.InterleaveLo(a3).ReshapeToUint64s(), a1.InterleaveHi(a3).ReshapeToUint64s()
		b4, b5, b6, b7 := a4.InterleaveLo(a6).ReshapeToUint64s(), a4.InterleaveHi(a6).ReshapeToUint64s(), a5.InterleaveLo(a7).ReshapeToUint64s(), a5.InterleaveHi(a7).ReshapeToUint64s()
		v0, v1, v2, v3 = b0.InterleaveLo(b4).ReshapeToUint16s().BitsToInt16(), b0.InterleaveHi(b4).ReshapeToUint16s().BitsToInt16(), b1.InterleaveLo(b5).ReshapeToUint16s().BitsToInt16(), b1.InterleaveHi(b5).ReshapeToUint16s().BitsToInt16()
		v4, v5, v6, v7 = b2.InterleaveLo(b6).ReshapeToUint16s().BitsToInt16(), b2.InterleaveHi(b6).ReshapeToUint16s().BitsToInt16(), b3.InterleaveLo(b7).ReshapeToUint16s().BitsToInt16(), b3.InterleaveHi(b7).ReshapeToUint16s().BitsToInt16()
	}
	{
		b0, b1 := v0.Add(v1), v0.Sub(v1)
		b2, b3 := v2.Add(v3), v2.Sub(v3)
		b4, b5 := v4.Add(v5), v4.Sub(v5)
		b6, b7 := v6.Add(v7), v6.Sub(v7)
		c0, c1, c2, c3 := b0.Add(b2), b1.Add(b3), b0.Sub(b2), b1.Sub(b3)
		c4, c5, c6, c7 := b4.Add(b6), b5.Add(b7), b4.Sub(b6), b5.Sub(b7)
		v0, v7, v3, v4, v2, v6, v1, v5 = c0.Add(c4), c1.Add(c5), c2.Add(c6), c3.Add(c7), c0.Sub(c4), c1.Sub(c5), c2.Sub(c6), c3.Sub(c7)
	}
	v0.StoreArray((*[8]int16)(coeff[0:8]))
	v1.StoreArray((*[8]int16)(coeff[8:16]))
	v2.StoreArray((*[8]int16)(coeff[16:24]))
	v3.StoreArray((*[8]int16)(coeff[24:32]))
	v4.StoreArray((*[8]int16)(coeff[32:40]))
	v5.StoreArray((*[8]int16)(coeff[40:48]))
	v6.StoreArray((*[8]int16)(coeff[48:56]))
	v7.StoreArray((*[8]int16)(coeff[56:64]))
}

// Hadamard is linear over the int16 ring, so intermediate wrapping has
// precisely the scalar result's final int16 conversion (libvpx v1.16.0).
func hadamardNetwork(v *[8]archsimd.Int16x8) {
	b0, b1 := v[0].Add(v[1]), v[0].Sub(v[1])
	b2, b3 := v[2].Add(v[3]), v[2].Sub(v[3])
	b4, b5 := v[4].Add(v[5]), v[4].Sub(v[5])
	b6, b7 := v[6].Add(v[7]), v[6].Sub(v[7])
	c0, c1, c2, c3 := b0.Add(b2), b1.Add(b3), b0.Sub(b2), b1.Sub(b3)
	c4, c5, c6, c7 := b4.Add(b6), b5.Add(b7), b4.Sub(b6), b5.Sub(b7)
	v[0], v[7], v[3], v[4], v[2], v[6], v[1], v[5] = c0.Add(c4), c1.Add(c5), c2.Add(c6), c3.Add(c7), c0.Sub(c4), c1.Sub(c5), c2.Sub(c6), c3.Sub(c7)
}
func transpose8(v *[8]archsimd.Int16x8) {
	a0, a1 := v[0].InterleaveLo(v[1]).ToBits().ReshapeToUint32s(), v[0].InterleaveHi(v[1]).ToBits().ReshapeToUint32s()
	a2, a3 := v[2].InterleaveLo(v[3]).ToBits().ReshapeToUint32s(), v[2].InterleaveHi(v[3]).ToBits().ReshapeToUint32s()
	a4, a5 := v[4].InterleaveLo(v[5]).ToBits().ReshapeToUint32s(), v[4].InterleaveHi(v[5]).ToBits().ReshapeToUint32s()
	a6, a7 := v[6].InterleaveLo(v[7]).ToBits().ReshapeToUint32s(), v[6].InterleaveHi(v[7]).ToBits().ReshapeToUint32s()
	b0, b1, b2, b3 := a0.InterleaveLo(a2).ReshapeToUint64s(), a0.InterleaveHi(a2).ReshapeToUint64s(), a1.InterleaveLo(a3).ReshapeToUint64s(), a1.InterleaveHi(a3).ReshapeToUint64s()
	b4, b5, b6, b7 := a4.InterleaveLo(a6).ReshapeToUint64s(), a4.InterleaveHi(a6).ReshapeToUint64s(), a5.InterleaveLo(a7).ReshapeToUint64s(), a5.InterleaveHi(a7).ReshapeToUint64s()
	v[0], v[1], v[2], v[3] = b0.InterleaveLo(b4).ReshapeToUint16s().BitsToInt16(), b0.InterleaveHi(b4).ReshapeToUint16s().BitsToInt16(), b1.InterleaveLo(b5).ReshapeToUint16s().BitsToInt16(), b1.InterleaveHi(b5).ReshapeToUint16s().BitsToInt16()
	v[4], v[5], v[6], v[7] = b2.InterleaveLo(b6).ReshapeToUint16s().BitsToInt16(), b2.InterleaveHi(b6).ReshapeToUint16s().BitsToInt16(), b3.InterleaveLo(b7).ReshapeToUint16s().BitsToInt16(), b3.InterleaveHi(b7).ReshapeToUint16s().BitsToInt16()
}
func hadamardGo(src []int16, stride int, coeff []int16, n int) bool {
	if !pixel.Enabled {
		return false
	}
	if n == 16 {
		hadamardGo(src, stride, coeff, 8)
		hadamardGo(src[8:], stride, coeff[64:], 8)
		hadamardGo(src[8*stride:], stride, coeff[128:], 8)
		hadamardGo(src[8*stride+8:], stride, coeff[192:], 8)
		for i := 0; i < 64; i += 4 {
			a, b, c, d := pixel.Load4(coeff[i:]), pixel.Load4(coeff[64+i:]), pixel.Load4(coeff[128+i:]), pixel.Load4(coeff[192+i:])
			u, v, w, z := a.Add(b).ShiftAllRight(1), a.Sub(b).ShiftAllRight(1), c.Add(d).ShiftAllRight(1), c.Sub(d).ShiftAllRight(1)
			pixel.Store4(coeff[i:], u.Add(w))
			pixel.Store4(coeff[64+i:], v.Add(z))
			pixel.Store4(coeff[128+i:], u.Sub(w))
			pixel.Store4(coeff[192+i:], v.Sub(z))
		}
		return true
	}
	var v [8]archsimd.Int16x8
	for i := range 8 {
		v[i] = archsimd.LoadInt16x8(src[i*stride:])
	}
	hadamardNetwork(&v)
	transpose8(&v)
	hadamardNetwork(&v)
	transpose8(&v)
	for i := range 8 {
		v[i].Store(coeff[i*8:])
	}
	return true
}
func forwardWHTGo(input []int16, stride int, output []int16) bool {
	if !pixel.Enabled || stride < 4 || len(input) < 3*stride+4 || len(output) < 16 {
		return false
	}
	a, b, c, d := pixel.Load4(input), pixel.Load4(input[stride:]), pixel.Load4(input[2*stride:]), pixel.Load4(input[3*stride:])
	a = a.Add(b)
	d = d.Sub(c)
	e := a.Sub(d).ShiftAllRight(1)
	b = e.Sub(b)
	c = e.Sub(c)
	a = a.Sub(c)
	d = d.Add(b)
	a, b, c, d = pixel.Transpose4(a, c, d, b)
	a = a.Add(b)
	d = d.Sub(c)
	e = a.Sub(d).ShiftAllRight(1)
	b = e.Sub(b)
	c = e.Sub(c)
	a = a.Sub(c)
	d = d.Add(b)
	a, b, c, d = pixel.Transpose4(a, c, d, b)
	pixel.Store4(output, a.ShiftAllLeft(2))
	pixel.Store4(output[4:], b.ShiftAllLeft(2))
	pixel.Store4(output[8:], c.ShiftAllLeft(2))
	pixel.Store4(output[12:], d.ShiftAllLeft(2))
	return true
}

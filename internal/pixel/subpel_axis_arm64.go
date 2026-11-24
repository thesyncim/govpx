//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// The public dispatcher proves the complete bilinear window and bounded
// positive geometry before these exact read-only loads. Each one-axis
// filtered sample lies in [-256,255], even for arbitrary int16 coefficients.
// With h<=64 each separate per-lane difference sum fits signed int16:
// -511*64=-32704 and 255*64=16320. Widen before merging the two halves.
func subpelStatsAxisHorizontal16(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	f0, f1, round := archsimd.BroadcastInt16x8(f[0]), archsimd.BroadcastInt16x8(f[1]), archsimd.BroadcastInt16x8(64)
	var sum, sse0, sse1 archsimd.Int32x4
	for x := 0; x < w; x += 16 {
		so, ro := x, x
		var sum0, sum1 archsimd.Int16x8
		y := 0
		for ; y+1 < h; y += 2 {
			{
				a := archsimd.LoadUint8x16(src[so:])
				b := archsimd.LoadUint8x16(src[so+1:])
				r := archsimd.LoadUint8x16(ref[ro:])
				v0 := filterShift7(mulAdd16(widen8(a).Mul(f0).Add(round), widen8(b), f1))
				v1 := filterShift7(mulAdd16(widen8(highBytes(a)).Mul(f0).Add(round), widen8(highBytes(b)), f1))
				d0, d1 := v0.Sub(widen8(r)), v1.Sub(widen8(highBytes(r)))
				sum0 = sum0.Add(d0)
				sum1 = sum1.Add(d1)
				sse0 = sse0.Add(square8(d0))
				sse1 = sse1.Add(square8(d1))
				so += ss
				ro += rs
			}
			{
				a := archsimd.LoadUint8x16(src[so:])
				b := archsimd.LoadUint8x16(src[so+1:])
				r := archsimd.LoadUint8x16(ref[ro:])
				v0 := filterShift7(mulAdd16(widen8(a).Mul(f0).Add(round), widen8(b), f1))
				v1 := filterShift7(mulAdd16(widen8(highBytes(a)).Mul(f0).Add(round), widen8(highBytes(b)), f1))
				d0, d1 := v0.Sub(widen8(r)), v1.Sub(widen8(highBytes(r)))
				sum0 = sum0.Add(d0)
				sum1 = sum1.Add(d1)
				sse0 = sse0.Add(square8(d0))
				sse1 = sse1.Add(square8(d1))
				so += ss
				ro += rs
			}
		}
		if y < h {
			a := archsimd.LoadUint8x16(src[so:])
			b := archsimd.LoadUint8x16(src[so+1:])
			r := archsimd.LoadUint8x16(ref[ro:])
			v0 := filterShift7(mulAdd16(widen8(a).Mul(f0).Add(round), widen8(b), f1))
			v1 := filterShift7(mulAdd16(widen8(highBytes(a)).Mul(f0).Add(round), widen8(highBytes(b)), f1))
			d0, d1 := v0.Sub(widen8(r)), v1.Sub(widen8(highBytes(r)))
			sum0 = sum0.Add(d0)
			sum1 = sum1.Add(d1)
			sse0 = sse0.Add(square8(d0))
			sse1 = sse1.Add(square8(d1))
			so += ss
			ro += rs
		}
		sum = sum.Add(sumDiff16(sum0)).Add(sumDiff16(sum1))
	}
	return int(sum32(sum)), uint32(sum32(sse0.Add(sse1)))
}
func subpelStatsAxisVertical16(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	f0, f1, round := archsimd.BroadcastInt16x8(f[0]), archsimd.BroadcastInt16x8(f[1]), archsimd.BroadcastInt16x8(64)
	var sum, sse0, sse1 archsimd.Int32x4
	for x := 0; x < w; x += 16 {
		so, ro := x, x
		var sum0, sum1 archsimd.Int16x8
		a := archsimd.LoadUint8x16(src[x:])
		so += ss
		y := 0
		for ; y+1 < h; y += 2 {
			{
				b := archsimd.LoadUint8x16(src[so:])
				r := archsimd.LoadUint8x16(ref[ro:])
				v0 := filterShift7(mulAdd16(widen8(a).Mul(f0).Add(round), widen8(b), f1))
				v1 := filterShift7(mulAdd16(widen8(highBytes(a)).Mul(f0).Add(round), widen8(highBytes(b)), f1))
				d0, d1 := v0.Sub(widen8(r)), v1.Sub(widen8(highBytes(r)))
				sum0 = sum0.Add(d0)
				sum1 = sum1.Add(d1)
				sse0 = sse0.Add(square8(d0))
				sse1 = sse1.Add(square8(d1))
				a = b
				so += ss
				ro += rs
			}
			{
				b := archsimd.LoadUint8x16(src[so:])
				r := archsimd.LoadUint8x16(ref[ro:])
				v0 := filterShift7(mulAdd16(widen8(a).Mul(f0).Add(round), widen8(b), f1))
				v1 := filterShift7(mulAdd16(widen8(highBytes(a)).Mul(f0).Add(round), widen8(highBytes(b)), f1))
				d0, d1 := v0.Sub(widen8(r)), v1.Sub(widen8(highBytes(r)))
				sum0 = sum0.Add(d0)
				sum1 = sum1.Add(d1)
				sse0 = sse0.Add(square8(d0))
				sse1 = sse1.Add(square8(d1))
				a = b
				so += ss
				ro += rs
			}
		}
		if y < h {
			b := archsimd.LoadUint8x16(src[so:])
			r := archsimd.LoadUint8x16(ref[ro:])
			v0 := filterShift7(mulAdd16(widen8(a).Mul(f0).Add(round), widen8(b), f1))
			v1 := filterShift7(mulAdd16(widen8(highBytes(a)).Mul(f0).Add(round), widen8(highBytes(b)), f1))
			d0, d1 := v0.Sub(widen8(r)), v1.Sub(widen8(highBytes(r)))
			sum0 = sum0.Add(d0)
			sum1 = sum1.Add(d1)
			sse0 = sse0.Add(square8(d0))
			sse1 = sse1.Add(square8(d1))
			a = b
			so += ss
			ro += rs
		}
		sum = sum.Add(sumDiff16(sum0)).Add(sumDiff16(sum1))
	}
	return int(sum32(sum)), uint32(sum32(sse0.Add(sse1)))
}

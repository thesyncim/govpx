//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// subpelStatsAxisHorizontal16 fuses 1D horizontal bilinear interpolation with
// (sum, sse) accumulation for 16-aligned widths (h <= 64).
// Half-pel uses NEON URHADD (Average) for 16 pixels per instruction; standard
// non-negative weights use uint8 MulWidenLo (UMULL).
func subpelStatsAxisHorizontal16(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	if f[0] == 64 && f[1] == 64 {
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			so, ro := x, x
			var sum0, sum1 archsimd.Int16x8
			for range h {
				row := src[so : so+17]
				pred := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row[1:17])))
				r := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
				d0 := pred.ExtendLo8ToUint16().BitsToInt16().Sub(r.ExtendLo8ToUint16().BitsToInt16())
				d1 := pred.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(r.HiToLo().ExtendLo8ToUint16().BitsToInt16())
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

	if f[0] >= 0 && f[1] >= 0 && int(f[0])+int(f[1]) <= 128 {
		u0, u1 := archsimd.BroadcastUint8x16(uint8(f[0])), archsimd.BroadcastUint8x16(uint8(f[1]))
		uround := archsimd.BroadcastUint16x8(64)
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			so, ro := x, x
			var sum0, sum1 archsimd.Int16x8
			for range h {
				row := src[so : so+17]
				a, b := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
				r := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
				v0 := a.MulWidenLo(u0).Add(b.MulWidenLo(u1)).Add(uround).ShiftAllRight(7).BitsToInt16()
				v1 := a.HiToLo().MulWidenLo(u0).Add(b.HiToLo().MulWidenLo(u1)).Add(uround).ShiftAllRight(7).BitsToInt16()
				d0 := v0.Sub(r.ExtendLo8ToUint16().BitsToInt16())
				d1 := v1.Sub(r.HiToLo().ExtendLo8ToUint16().BitsToInt16())
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

	f0, f1, round := archsimd.BroadcastInt16x8(f[0]), archsimd.BroadcastInt16x8(f[1]), archsimd.BroadcastInt16x8(64)
	var sum, sse0, sse1 archsimd.Int32x4
	for x := 0; x < w; x += 16 {
		so, ro := x, x
		var sum0, sum1 archsimd.Int16x8
		for range h {
			row := src[so : so+17]
			a, b := archsimd.LoadUint8x16(row[0:16]), archsimd.LoadUint8x16(row[1:17])
			r := archsimd.LoadUint8x16(ref[ro : ro+16])
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

// subpelStatsAxisVertical16 fuses 1D vertical bilinear interpolation with
// (sum, sse) accumulation for 16-aligned widths (h <= 64).
func subpelStatsAxisVertical16(src []byte, ss int, ref []byte, rs, w, h int, f [2]int16) (int, uint32) {
	if f[0] == 64 && f[1] == 64 {
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			so, ro := x+ss, x
			var sum0, sum1 archsimd.Int16x8
			a := archsimd.LoadUint8x16Array((*[16]byte)(src[x : x+16]))
			for range h {
				b := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				pred := a.Average(b)
				a = b
				r := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
				d0 := pred.ExtendLo8ToUint16().BitsToInt16().Sub(r.ExtendLo8ToUint16().BitsToInt16())
				d1 := pred.HiToLo().ExtendLo8ToUint16().BitsToInt16().Sub(r.HiToLo().ExtendLo8ToUint16().BitsToInt16())
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

	if f[0] >= 0 && f[1] >= 0 && int(f[0])+int(f[1]) <= 128 {
		u0, u1 := archsimd.BroadcastUint8x16(uint8(f[0])), archsimd.BroadcastUint8x16(uint8(f[1]))
		uround := archsimd.BroadcastUint16x8(64)
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			so, ro := x+ss, x
			var sum0, sum1 archsimd.Int16x8
			a := archsimd.LoadUint8x16Array((*[16]byte)(src[x : x+16]))
			for range h {
				b := archsimd.LoadUint8x16Array((*[16]byte)(src[so : so+16]))
				r := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
				v0 := a.MulWidenLo(u0).Add(b.MulWidenLo(u1)).Add(uround).ShiftAllRight(7).BitsToInt16()
				v1 := a.HiToLo().MulWidenLo(u0).Add(b.HiToLo().MulWidenLo(u1)).Add(uround).ShiftAllRight(7).BitsToInt16()
				a = b
				d0 := v0.Sub(r.ExtendLo8ToUint16().BitsToInt16())
				d1 := v1.Sub(r.HiToLo().ExtendLo8ToUint16().BitsToInt16())
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

	f0, f1, round := archsimd.BroadcastInt16x8(f[0]), archsimd.BroadcastInt16x8(f[1]), archsimd.BroadcastInt16x8(64)
	var sum, sse0, sse1 archsimd.Int32x4
	for x := 0; x < w; x += 16 {
		so, ro := x+ss, x
		var sum0, sum1 archsimd.Int16x8
		a := archsimd.LoadUint8x16(src[x : x+16])
		for range h {
			b := archsimd.LoadUint8x16(src[so : so+16])
			r := archsimd.LoadUint8x16(ref[ro : ro+16])
			v0 := filterShift7(mulAdd16(widen8(a).Mul(f0).Add(round), widen8(b), f1))
			v1 := filterShift7(mulAdd16(widen8(highBytes(a)).Mul(f0).Add(round), widen8(highBytes(b)), f1))
			a = b
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

// subpelStatsAxisHorizontal8 fuses 1D horizontal bilinear interpolation with
// (sum, sse) accumulation for w == 8 (h <= 64) using URHADD / UMULL.
func subpelStatsAxisHorizontal8(src []byte, ss int, ref []byte, rs, h int, f [2]int16) (int, uint32) {
	if f[0] == 64 && f[1] == 64 {
		var sum0 archsimd.Int16x8
		var sse archsimd.Int32x4
		so, ro := 0, 0
		for range h {
			a, b := load8(src[so:]), load8(src[so+1:])
			r := load8(ref[ro:])
			pred := a.Average(b)
			d := pred.ExtendLo8ToUint16().BitsToInt16().Sub(r.ExtendLo8ToUint16().BitsToInt16())
			sum0 = sum0.Add(d)
			sse = sse.Add(square8(d))
			so += ss
			ro += rs
		}
		return int(sum32(sumDiff16(sum0))), uint32(sum32(sse))
	}
	if f[0] >= 0 && f[1] >= 0 && int(f[0])+int(f[1]) <= 128 {
		u0, u1 := archsimd.BroadcastUint8x16(uint8(f[0])), archsimd.BroadcastUint8x16(uint8(f[1]))
		uround := archsimd.BroadcastUint16x8(64)
		var sum0 archsimd.Int16x8
		var sse archsimd.Int32x4
		so, ro := 0, 0
		for range h {
			a, b := load8(src[so:]), load8(src[so+1:])
			r := load8(ref[ro:])
			v := a.MulWidenLo(u0).Add(b.MulWidenLo(u1)).Add(uround).ShiftAllRight(7).BitsToInt16()
			d := v.Sub(r.ExtendLo8ToUint16().BitsToInt16())
			sum0 = sum0.Add(d)
			sse = sse.Add(square8(d))
			so += ss
			ro += rs
		}
		return int(sum32(sumDiff16(sum0))), uint32(sum32(sse))
	}
	return subpelStatsKernel[[8]byte](src, ss, ref, rs, 8, h, f, [2]int16{128, 0})
}

// subpelStatsAxisVertical8 fuses 1D vertical bilinear interpolation with
// (sum, sse) accumulation for w == 8 (h <= 64) using URHADD / UMULL.
func subpelStatsAxisVertical8(src []byte, ss int, ref []byte, rs, h int, f [2]int16) (int, uint32) {
	if f[0] == 64 && f[1] == 64 {
		var sum0 archsimd.Int16x8
		var sse archsimd.Int32x4
		a := load8(src)
		so, ro := ss, 0
		for range h {
			b := load8(src[so:])
			r := load8(ref[ro:])
			pred := a.Average(b)
			a = b
			d := pred.ExtendLo8ToUint16().BitsToInt16().Sub(r.ExtendLo8ToUint16().BitsToInt16())
			sum0 = sum0.Add(d)
			sse = sse.Add(square8(d))
			so += ss
			ro += rs
		}
		return int(sum32(sumDiff16(sum0))), uint32(sum32(sse))
	}
	if f[0] >= 0 && f[1] >= 0 && int(f[0])+int(f[1]) <= 128 {
		u0, u1 := archsimd.BroadcastUint8x16(uint8(f[0])), archsimd.BroadcastUint8x16(uint8(f[1]))
		uround := archsimd.BroadcastUint16x8(64)
		var sum0 archsimd.Int16x8
		var sse archsimd.Int32x4
		a := load8(src)
		so, ro := ss, 0
		for range h {
			b := load8(src[so:])
			r := load8(ref[ro:])
			v := a.MulWidenLo(u0).Add(b.MulWidenLo(u1)).Add(uround).ShiftAllRight(7).BitsToInt16()
			a = b
			d := v.Sub(r.ExtendLo8ToUint16().BitsToInt16())
			sum0 = sum0.Add(d)
			sse = sse.Add(square8(d))
			so += ss
			ro += rs
		}
		return int(sum32(sumDiff16(sum0))), uint32(sum32(sse))
	}
	return subpelStatsKernel[[8]byte](src, ss, ref, rs, 8, h, [2]int16{128, 0}, f)
}

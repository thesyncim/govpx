//go:build goexperiment.simd && arm64 && !purego

package pixel

import "simd/archsimd"

// subpelStats16 fuses 2D bilinear interpolation with (sum, sse) variance
// accumulation for 16-aligned widths. Half-pel axes use NEON URHADD (Average)
// to interpolate 16 pixels in a single instruction.
func subpelStats16(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (int, uint32) {
	_ = src[h*ss+w]
	_ = ref[(h-1)*rs+w-1]

	// Fast path: 2D half-pel (fx == [64,64] && fy == [64,64]).
	// ((a+b+1)>>1) on each axis is bit-exact to NEON URHADD (Average).
	if fx[0] == 64 && fx[1] == 64 && fy[0] == 64 && fy[1] == 64 && h <= 64 {
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			var sum0, sum1 archsimd.Int16x8
			row0 := src[x : x+17]
			prev := archsimd.LoadUint8x16Array((*[16]byte)(row0[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row0[1:17])))
			so, ro := ss+x, x
			for range h {
				row := src[so : so+17]
				cur := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row[1:17])))
				pred := prev.Average(cur)
				prev = cur
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

	// Fast path: 1-axis half-pel (horizontal [64,64], vertical quarter-pel).
	if fx[0] == 64 && fx[1] == 64 && fy[0] >= 0 && fy[1] >= 0 && int(fy[0])+int(fy[1]) <= 128 && h <= 64 {
		uy0, uy1 := archsimd.BroadcastUint8x16(uint8(fy[0])), archsimd.BroadcastUint8x16(uint8(fy[1]))
		uround := archsimd.BroadcastUint16x8(64)
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			var sum0, sum1 archsimd.Int16x8
			row0 := src[x : x+17]
			prev := archsimd.LoadUint8x16Array((*[16]byte)(row0[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row0[1:17])))
			prevH := prev.HiToLo()
			so, ro := ss+x, x
			for range h {
				row := src[so : so+17]
				cur := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])).Average(archsimd.LoadUint8x16Array((*[16]byte)(row[1:17])))
				curH := cur.HiToLo()
				v0 := prev.MulWidenLo(uy0).Add(cur.MulWidenLo(uy1)).Add(uround).ShiftAllRight(7)
				v1 := prevH.MulWidenLo(uy0).Add(curH.MulWidenLo(uy1)).Add(uround).ShiftAllRight(7)
				prev, prevH = cur, curH
				r := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
				d0 := v0.BitsToInt16().Sub(r.ExtendLo8ToUint16().BitsToInt16())
				d1 := v1.BitsToInt16().Sub(r.HiToLo().ExtendLo8ToUint16().BitsToInt16())
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

	// Fast path: 1-axis half-pel (vertical [64,64], horizontal quarter-pel).
	if fy[0] == 64 && fy[1] == 64 && fx[0] >= 0 && fx[1] >= 0 && int(fx[0])+int(fx[1]) <= 128 && h <= 64 {
		ux0, ux1 := archsimd.BroadcastUint8x16(uint8(fx[0])), archsimd.BroadcastUint8x16(uint8(fx[1]))
		uround := archsimd.BroadcastUint16x8(64)
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			var sum0, sum1 archsimd.Int16x8
			row0 := src[x : x+17]
			r0, r1 := archsimd.LoadUint8x16Array((*[16]byte)(row0[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row0[1:17]))
			h0 := r0.MulWidenLo(ux0).Add(r1.MulWidenLo(ux1)).Add(uround).ShiftAllRight(7)
			h1 := r0.HiToLo().MulWidenLo(ux0).Add(r1.HiToLo().MulWidenLo(ux1)).Add(uround).ShiftAllRight(7)
			prev := h0.ReshapeToUint8s().ConcatEven(h1.ReshapeToUint8s())
			so, ro := ss+x, x
			for range h {
				row := src[so : so+17]
				r0, r1 := archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
				h0 := r0.MulWidenLo(ux0).Add(r1.MulWidenLo(ux1)).Add(uround).ShiftAllRight(7)
				h1 := r0.HiToLo().MulWidenLo(ux0).Add(r1.HiToLo().MulWidenLo(ux1)).Add(uround).ShiftAllRight(7)
				cur := h0.ReshapeToUint8s().ConcatEven(h1.ReshapeToUint8s())
				pred := prev.Average(cur)
				prev = cur
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

	// Non-negative standard bilinear weights (0..128) use uint8 MulWidenLo (UMULL)
	// directly without separate uint8->int16 widening.
	if fx[0] >= 0 && fx[1] >= 0 && fy[0] >= 0 && fy[1] >= 0 &&
		int(fx[0])+int(fx[1]) <= 128 && int(fy[0])+int(fy[1]) <= 128 && h <= 64 {
		ux0, ux1 := archsimd.BroadcastUint8x16(uint8(fx[0])), archsimd.BroadcastUint8x16(uint8(fx[1]))
		uy0, uy1 := archsimd.BroadcastUint8x16(uint8(fy[0])), archsimd.BroadcastUint8x16(uint8(fy[1]))
		uround := archsimd.BroadcastUint16x8(64)
		var sum, sse0, sse1 archsimd.Int32x4
		for x := 0; x < w; x += 16 {
			var sum0, sum1 archsimd.Int16x8
			row0 := src[x : x+17]
			r0, r1 := archsimd.LoadUint8x16Array((*[16]byte)(row0[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row0[1:17]))
			prevLo := r0.MulWidenLo(ux0).Add(r1.MulWidenLo(ux1)).Add(uround).ShiftAllRight(7).TruncToUint8()
			prevHi := r0.HiToLo().MulWidenLo(ux0).Add(r1.HiToLo().MulWidenLo(ux1)).Add(uround).ShiftAllRight(7).TruncToUint8()
			so, ro := ss+x, x
			for range h {
				row := src[so : so+17]
				r0, r1 = archsimd.LoadUint8x16Array((*[16]byte)(row[0:16])), archsimd.LoadUint8x16Array((*[16]byte)(row[1:17]))
				curLo := r0.MulWidenLo(ux0).Add(r1.MulWidenLo(ux1)).Add(uround).ShiftAllRight(7).TruncToUint8()
				curHi := r0.HiToLo().MulWidenLo(ux0).Add(r1.HiToLo().MulWidenLo(ux1)).Add(uround).ShiftAllRight(7).TruncToUint8()
				v0 := prevLo.MulWidenLo(uy0).Add(curLo.MulWidenLo(uy1)).Add(uround).ShiftAllRight(7).BitsToInt16()
				v1 := prevHi.MulWidenLo(uy0).Add(curHi.MulWidenLo(uy1)).Add(uround).ShiftAllRight(7).BitsToInt16()
				prevLo, prevHi = curLo, curHi
				r := archsimd.LoadUint8x16Array((*[16]byte)(ref[ro : ro+16]))
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

	x0, x1, y0, y1 := archsimd.BroadcastInt16x8(fx[0]), archsimd.BroadcastInt16x8(fx[1]), archsimd.BroadcastInt16x8(fy[0]), archsimd.BroadcastInt16x8(fy[1])
	round := archsimd.BroadcastInt16x8(64)
	var sum, sse archsimd.Int32x4
	for x := 0; x < w; x += 16 {
		r0, r1 := archsimd.LoadUint8x16(src[x:x+16]), archsimd.LoadUint8x16(src[x+1:x+17])
		a0 := filterShift7(mulAdd16(widen8(r0).Mul(x0).Add(round), widen8(r1), x1))
		a1 := filterShift7(mulAdd16(widen8(highBytes(r0)).Mul(x0).Add(round), widen8(highBytes(r1)), x1))
		so, ro := ss+x, x
		for range h {
			r0, r1 = archsimd.LoadUint8x16(src[so:so+16]), archsimd.LoadUint8x16(src[so+1:so+17])
			b0 := filterShift7(mulAdd16(widen8(r0).Mul(x0).Add(round), widen8(r1), x1))
			b1 := filterShift7(mulAdd16(widen8(highBytes(r0)).Mul(x0).Add(round), widen8(highBytes(r1)), x1))
			v0 := filterShift7(mulAdd16(a0.Mul(y0).Add(round), b0, y1))
			v1 := filterShift7(mulAdd16(a1.Mul(y0).Add(round), b1, y1))
			a0, a1 = b0, b1
			r := archsimd.LoadUint8x16(ref[ro : ro+16])
			d0, d1 := v0.Sub(widen8(r)), v1.Sub(widen8(highBytes(r)))
			sum = sum.Add(sumDiff16(d0.Add(d1)))
			sse = sse.Add(square8(d0)).Add(square8(d1))
			so += ss
			ro += rs
		}
	}
	return int(sum32(sum)), uint32(sum32(sse))
}

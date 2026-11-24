//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

func sadGeneric(src []byte, ss int, ref []byte, rs, w, h int) int {
	var acc archsimd.Uint32x4
	tail := 0
	for y := range h {
		a, b := src[y*ss:], ref[y*rs:]
		x := 0
		for ; x+16 <= w; x += 16 {
			acc = sadAdd(acc, archsimd.LoadUint8x16(a[x:]), archsimd.LoadUint8x16(b[x:]))
		}
		if x+8 <= w {
			acc = sadAdd(acc, load8(a[x:]), load8(b[x:]))
			x += 8
		}
		for ; x < w; x++ {
			d := int(a[x]) - int(b[x])
			tail += max(d, -d)
		}
	}
	return int(sumU32(acc)) + tail
}
func Stats(src []byte, ss int, ref []byte, rs, w, h int) (int, uint32) {
	if !Enabled {
		return StatsScalar(src, ss, ref, rs, w, h)
	}

	if (w == 4 && h%4 == 0 || w == 8 || w >= 16 && w%16 == 0) && w <= 64 {
		if h <= 4096 {
			sse, sum := pixelStatsSIMD(src, ss, ref, rs, w, h)
			return int(sum), sse
		}
		return statsTallSIMD(src, ss, ref, rs, w, h)
	}

	var sums, squares archsimd.Int32x4
	tailSum := 0
	var tailSSE uint32
	for y := range h {
		a, b := src[y*ss:], ref[y*rs:]
		x := 0
		for ; x+8 <= w; x += 8 {
			d := widen8(load8(a[x:])).Sub(widen8(load8(b[x:])))
			sums = sums.Add(sumDiff16(d))
			squares = squares.Add(square8(d))
		}
		if x+4 <= w {
			d := LoadPixels4(a[x:]).Sub(LoadPixels4(b[x:]))
			sums = sums.Add(d)
			squares = squares.Add(d.Mul(d))
			x += 4
		}
		for ; x < w; x++ {
			d := int(a[x]) - int(b[x])
			tailSum += d
			tailSSE += uint32(d * d)
		}
	}
	return int(sum32(sums)) + tailSum, uint32(sum32(squares)) + tailSSE
}
func Subtract(src []byte, ss int, pred []byte, ps int, out []int16, os, w, h int) bool {
	if !Enabled {
		return SubtractScalar(src, ss, pred, ps, out, os, w, h)
	}

	if (w == 4 || w == 8 || (w >= 16 && w <= 64 && w%16 == 0)) && h > 0 && h <= 64 && (uint(ss)|uint(ps)|uint(os))>>30 == 0 {
		if nz, ok := subtractRowsBounded(src, ss, pred, ps, out, os, w, h); ok {
			return nz
		}
	}

	var bits archsimd.Int16x8
	tail := int16(0)
	for y := range h {
		a, b, dst := src[y*ss:], pred[y*ps:], out[y*os:]
		x := 0
		for ; x+8 <= w; x += 8 {
			d := widen8(load8(a[x:])).Sub(widen8(load8(b[x:])))
			d.Store(dst[x:])
			bits = bits.Or(d)
		}
		for ; x < w; x++ {
			d := int16(a[x]) - int16(b[x])
			dst[x] = d
			tail |= d
		}
	}
	return bits.ToBits().ReshapeToUint64s().GetElem(0)|bits.ToBits().ReshapeToUint64s().GetElem(1) != 0 || tail != 0
}

// Filter uses widening int32 products: negative filter lobes cannot overflow
// or saturate before the codec's single rounding and clipping operation.
func Filter(src []byte, ss int, dst []byte, ds, w, h, step int, taps []int16) {
	if !Enabled {
		FilterScalar(src, ss, dst, ds, w, h, step, taps)
		return
	}

	if len(taps) == 6 && filterSixNarrow8(src, ss, dst, ds, w, h, step, taps) {
		return
	}

	if len(taps) == 2 && filterLinear8(src, ss, dst, ds, w, h, step, taps) {
		return
	}

	if filterWide8(src, ss, dst, ds, w, h, step, taps) {
		return
	}

	if filterNarrow8(src, ss, dst, ds, w, h, step, taps) {
		return
	}

	if step == ss && filterVertical8(src, ss, dst, ds, w, h, taps) {
		return
	}

	if w == 4 {
		bias := archsimd.BroadcastInt32x4(64)
		var c [8]archsimd.Int32x4
		for k, v := range taps {
			c[k] = archsimd.BroadcastInt32x4(int32(v))
		}
		for y := range h {
			sum := bias
			for k := range taps {
				sum = sum.Add(LoadPixels4(src[y*ss+k*step:]).Mul(c[k]))
			}
			StorePixels4(dst[y*ds:], sum.ShiftAllRight(7))
		}
		return
	}

	var c [8]archsimd.Int16x8
	for i, v := range taps {
		c[i] = archsimd.BroadcastInt16x8(v)
	}
	bias := archsimd.BroadcastInt32x4(64)
	for y := range h {
		a, b := src[y*ss:], dst[y*ds:]
		x := 0
		for ; x+8 <= w; x += 8 {
			var lo, hi archsimd.Int32x4
			if step == 1 {
				lo, hi = u8SumRowX(a[x:], &c, len(taps))
			} else {
				lo, hi = u8SumColY(a[x:], step, &c, len(taps))
			}
			lo, hi = lo.Add(bias), hi.Add(bias)
			store8(b[x:], lo.ShiftAllRight(7), hi.ShiftAllRight(7))
		}
		for ; x < w; x++ {
			v := 64
			for k, c := range taps {
				v += int(a[x+k*step]) * int(c)
			}
			b[x] = byte(min(max(v>>7, 0), 255))
		}
	}
}
func Average(src []byte, ss int, dst []byte, ds, w, h int) {
	if !Enabled {
		AverageScalar(src, ss, dst, ds, w, h)
		return
	}

	// Small rows contain no vector packet. Avoid rechecking the packet loop
	// at every row while retaining the original scalar read/store order.
	if w < 8 {
		for y := range h {
			a, b := src[y*ss:], dst[y*ds:]
			for x := 0; x < w; x++ {
				b[x] = byte((int(a[x]) + int(b[x]) + 1) >> 1)
			}
		}
		return
	}

	for y := range h {
		a, b := src[y*ss:], dst[y*ds:]
		x := 0
		for ; x+8 <= w; x += 8 {
			v := load8(a[x:]).Average(load8(b[x:]))
			storeBytes8(b[x:], v)
		}
		for ; x < w; x++ {
			b[x] = byte((int(a[x]) + int(b[x]) + 1) >> 1)
		}
	}
}

// Error widens before subtraction, preserving the full signed int16 domain.
// Four squares fit in uint64; no int32 horizontal accumulation can overflow.
func Error(a, b []int16, n int) (sum uint64) {
	if !Enabled {
		return ErrorScalar(a, b, n)
	}

	i := 0
	for ; i+8 <= n; i += 8 {
		va, vb := archsimd.LoadInt16x8(a[i:]), archsimd.LoadInt16x8(b[i:])
		lo := va.ExtendLo4ToInt32().Sub(vb.ExtendLo4ToInt32())
		hi := high4(va).Sub(high4(vb))
		sum += sumSquares(lo) + sumSquares(hi)
	}
	for ; i < n; i++ {
		d := int64(a[i]) - int64(b[i])
		sum += uint64(d * d)
	}
	return
}

func Quantize(coeff, iscan, qcoeff, dqcoeff []int16, n, zbin, round, quant, shift, dequant int, fp bool) (eob int) {
	if !Enabled {
		return QuantizeScalar(coeff, iscan, qcoeff, dqcoeff, n, zbin, round, quant, shift, dequant, fp)
	}

	z, r, q, s, d := archsimd.BroadcastInt32x4(int32(zbin)), archsimd.BroadcastInt32x4(int32(round)), archsimd.BroadcastInt32x4(int32(quant)), archsimd.BroadcastInt32x4(int32(shift)), archsimd.BroadcastInt32x4(int32(dequant))
	zero := archsimd.Int32x4{}
	max16 := archsimd.BroadcastInt32x4(32767)
	min16 := archsimd.BroadcastInt32x4(-32768)
	var last archsimd.Int32x4
	i := 0
	for ; i+4 <= n; i += 4 {
		c := Load4(coeff[i:])
		a := c.Abs()
		v := a.Add(r)
		keep := a.GreaterEqual(z)
		if fp {
			keep = v.GreaterEqual(d)
		}
		v = v.Min(max16).Max(min16)
		t := v.Mul(q).ShiftAllRight(16)
		if !fp {
			t = t.Add(v).Mul(s).ShiftAllRight(16)
		}
		t = t.Masked(keep)
		t = zero.Sub(t).IfElse(c.Less(zero), t)
		if qcoeff != nil {
			Store4(qcoeff[i:], t)
		}
		Store4(dqcoeff[i:], t.Mul(d))
		scan := Load4(iscan[i:])
		last = last.Max(scan.Masked(t.NotEqual(zero)))
	}
	eob = int(Max4(last))
	for ; i < n; i++ {
		c := int(coeff[i])
		a := max(c, -c)
		t := 0
		if (fp && a+round >= dequant) || (!fp && a >= zbin) {
			v := min(max(a+round, -32768), 32767)
			if fp {
				t = v * quant >> 16
			} else {
				t = (((v * quant >> 16) + v) * shift) >> 16
			}
			if c < 0 {
				t = -t
			}
		}
		if qcoeff != nil {
			qcoeff[i] = int16(t)
		}
		dqcoeff[i] = int16(t * dequant)
		if t != 0 {
			eob = max(eob, int(iscan[i]))
		}
	}
	return
}

// Gather writes residuals in VP8's 4x4 block scan order, without staging
// complete macroblocks or allocating packing buffers. Output must not overlap
// either input; the read-only input planes may alias each other.
func Gather(src []byte, ss int, pred []byte, ps int, out []int16, w int) {
	if !Enabled {
		GatherScalar(src, ss, pred, ps, out, w)
		return
	}

	// Pair adjacent rows inside each four-row block group. Interleaving
	// four-coefficient packets halves stores and avoids scalar extraction.
	// The bounded block index and initial output check prove every store fits.
	if w == 16 {
		_ = out[255]
		for block := range 4 {
			y := block * 4
			{
				a0, b0 := archsimd.LoadUint8x16(src[y*ss:]), archsimd.LoadUint8x16(pred[y*ps:])
				a1, b1 := archsimd.LoadUint8x16(src[(y+1)*ss:]), archsimd.LoadUint8x16(pred[(y+1)*ps:])
				lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
				lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
				hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
				hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
				off := block * 64
				lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().Store(out[off:])
				lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().Store(out[off+16:])
				hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().Store(out[off+32:])
				hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().Store(out[off+48:])
			}
			{
				a0, b0 := archsimd.LoadUint8x16(src[(y+2)*ss:]), archsimd.LoadUint8x16(pred[(y+2)*ps:])
				a1, b1 := archsimd.LoadUint8x16(src[(y+3)*ss:]), archsimd.LoadUint8x16(pred[(y+3)*ps:])
				lo0 := widen8(a0).Sub(widen8(b0)).ToBits().ReshapeToUint64s()
				lo1 := widen8(a1).Sub(widen8(b1)).ToBits().ReshapeToUint64s()
				hi0 := widenHi8(a0).Sub(widenHi8(b0)).ReshapeToUint64s()
				hi1 := widenHi8(a1).Sub(widenHi8(b1)).ReshapeToUint64s()
				off := block*64 + 8
				lo0.InterleaveLo(lo1).ReshapeToUint16s().BitsToInt16().Store(out[off:])
				lo0.InterleaveHi(lo1).ReshapeToUint16s().BitsToInt16().Store(out[off+16:])
				hi0.InterleaveLo(hi1).ReshapeToUint16s().BitsToInt16().Store(out[off+32:])
				hi0.InterleaveHi(hi1).ReshapeToUint16s().BitsToInt16().Store(out[off+48:])
			}
		}
		return
	}

	if w == 8 {
		_ = out[63]
		for block := range 2 {
			y := block * 4
			{
				a := packRows8(src, y*ss, ss)
				b := packRows8(pred, y*ps, ps)
				lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
				hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
				off := block * 32
				lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().Store(out[off:])
				lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().Store(out[off+16:])
			}
			{
				a := packRows8(src, (y+2)*ss, ss)
				b := packRows8(pred, (y+2)*ps, ps)
				lo := widen8(a).Sub(widen8(b)).ToBits().ReshapeToUint64s()
				hi := widenHi8(a).Sub(widenHi8(b)).ReshapeToUint64s()
				off := block*32 + 8
				lo.InterleaveLo(hi).ReshapeToUint16s().BitsToInt16().Store(out[off:])
				lo.InterleaveHi(hi).ReshapeToUint16s().BitsToInt16().Store(out[off+16:])
			}
		}
		return
	}

	for y := range w {
		a, b := src[y*ss:], pred[y*ps:]
		for x := 0; x < w; x += 8 {
			v := widen8(load8(a[x:])).Sub(widen8(load8(b[x:]))).ToBits().ReshapeToUint64s()
			off := ((y/4)*(w/4)+x/4)*16 + (y%4)*4
			put4(out[off:], v.GetElem(0))
			put4(out[off+16:], v.GetElem(1))
		}
	}
}
func Temporal(src []byte, ss int, pred []byte, ps, w, strength, weight int, acc, count []uint32) {
	if !Enabled {
		TemporalScalar(src, ss, pred, ps, w, strength, weight, acc, count)
		return
	}

	rounding := int32(0)
	if strength > 0 {
		rounding = 1 << (strength - 1)
	}
	r := archsimd.BroadcastInt32x4(rounding)
	three := archsimd.BroadcastInt32x4(3)
	sixteen := archsimd.BroadcastInt32x4(16)
	wt := archsimd.BroadcastInt32x4(int32(weight))
	for y := range w {
		for x := 0; x < w; x += 4 {
			a, b := LoadPixels4(src[y*ss+x:]), LoadPixels4(pred[y*ps+x:])
			d := a.Sub(b)
			m := sixteen.Sub(d.Mul(d).Mul(three).Add(r).ShiftAllRight(uint64(strength)).Min(sixteen)).Mul(wt).ToBits()
			i := y*w + x
			m.Add(archsimd.LoadUint32x4(count[i:])).Store(count[i:])
			m.Mul(b.ToBits()).Add(archsimd.LoadUint32x4(acc[i:])).Store(acc[i:])
		}
	}
}
func VP8Quant(coeff, round, quant, dequant []int16, qcoeff, dqcoeff []int16) {
	if !Enabled {
		VP8QuantScalar(coeff, round, quant, dequant, qcoeff, dqcoeff)
		return
	}

	for i := 0; i < 16; i += 4 {
		c := Load4(coeff[i:])
		sign := c.ShiftAllRight(31)
		q := c.Abs().Add(Load4(round[i:])).Mul(Load4(quant[i:])).ShiftAllRight(16)
		q = q.Xor(sign).Sub(sign)
		Store4(qcoeff[i:], q)
		Store4(dqcoeff[i:], q.Mul(Load4(dequant[i:])))
	}
}
func AddConstant(pred []byte, ps int, dst []byte, ds, w, h, delta int) {
	if !Enabled {
		AddConstantScalar(pred, ps, dst, ds, w, h, delta)
		return
	}
	if w <= 0 || h <= 0 {
		return
	}
	if delta == 0 && ps == ds && len(pred) > 0 && len(dst) > 0 && &pred[0] == &dst[0] {
		return
	}
	_ = pred[(h-1)*ps+w-1]
	_ = dst[(h-1)*ds+w-1]
	canBatchRows := ps >= w && ds >= w &&
		((&pred[0] == &dst[0] && ps == ds) || !slicesOverlap(pred, dst, (h-1)*ps+w, (h-1)*ds+w))

	// Saturating 8-bit add/sub (UQADD/UQSUB on ARM64, PADDUSB/PSUBUSB on AMD64)
	// processes 16 pixels per instruction without widening to int32.
	if delta >= 0 {
		d := uint8(255)
		if delta < 255 {
			d = uint8(delta)
		}
		dv := archsimd.BroadcastUint8x16(d)
		if canBatchRows && w == 4 && h == 4 {
			v := archsimd.LoadUint32x4Array(&[4]uint32{
				binary.LittleEndian.Uint32(pred[0:]),
				binary.LittleEndian.Uint32(pred[ps:]),
				binary.LittleEndian.Uint32(pred[2*ps:]),
				binary.LittleEndian.Uint32(pred[3*ps:]),
			}).ReshapeToUint8s().AddSaturated(dv).ReshapeToUint32s()
			binary.LittleEndian.PutUint32(dst[0:], v.GetElem(0))
			binary.LittleEndian.PutUint32(dst[ds:], v.GetElem(1))
			binary.LittleEndian.PutUint32(dst[2*ds:], v.GetElem(2))
			binary.LittleEndian.PutUint32(dst[3*ds:], v.GetElem(3))
			return
		}
		if canBatchRows && w == 8 {
			po, do := 0, 0
			y := 0
			for ; y+1 < h; y += 2 {
				v := archsimd.LoadUint64x2Array(&[2]uint64{
					binary.LittleEndian.Uint64(pred[po:]),
					binary.LittleEndian.Uint64(pred[po+ps:]),
				}).ReshapeToUint8s().AddSaturated(dv).ReshapeToUint64s()
				binary.LittleEndian.PutUint64(dst[do:], v.GetElem(0))
				binary.LittleEndian.PutUint64(dst[do+ds:], v.GetElem(1))
				po += 2 * ps
				do += 2 * ds
			}
			if y < h {
				storeBytes8(dst[do:], load8(pred[po:]).AddSaturated(dv))
			}
			return
		}
		for y := range h {
			po, do := y*ps, y*ds
			x := 0
			for ; x+16 <= w; x += 16 {
				archsimd.LoadUint8x16Array((*[16]byte)(pred[po+x : po+x+16])).AddSaturated(dv).StoreArray((*[16]byte)(dst[do+x : do+x+16]))
			}
			for ; x+8 <= w; x += 8 {
				storeBytes8(dst[do+x:], load8(pred[po+x:]).AddSaturated(dv))
			}
			for ; x < w; x += 4 {
				StorePixels4(dst[do+x:], LoadPixels4(pred[po+x:]).Add(archsimd.BroadcastInt32x4(int32(delta))))
			}
		}
		return
	}

	d := uint8(255)
	if -delta < 255 {
		d = uint8(-delta)
	}
	dv := archsimd.BroadcastUint8x16(d)
	if canBatchRows && w == 4 && h == 4 {
		v := archsimd.LoadUint32x4Array(&[4]uint32{
			binary.LittleEndian.Uint32(pred[0:]),
			binary.LittleEndian.Uint32(pred[ps:]),
			binary.LittleEndian.Uint32(pred[2*ps:]),
			binary.LittleEndian.Uint32(pred[3*ps:]),
		}).ReshapeToUint8s().SubSaturated(dv).ReshapeToUint32s()
		binary.LittleEndian.PutUint32(dst[0:], v.GetElem(0))
		binary.LittleEndian.PutUint32(dst[ds:], v.GetElem(1))
		binary.LittleEndian.PutUint32(dst[2*ds:], v.GetElem(2))
		binary.LittleEndian.PutUint32(dst[3*ds:], v.GetElem(3))
		return
	}
	if canBatchRows && w == 8 {
		po, do := 0, 0
		y := 0
		for ; y+1 < h; y += 2 {
			v := archsimd.LoadUint64x2Array(&[2]uint64{
				binary.LittleEndian.Uint64(pred[po:]),
				binary.LittleEndian.Uint64(pred[po+ps:]),
			}).ReshapeToUint8s().SubSaturated(dv).ReshapeToUint64s()
			binary.LittleEndian.PutUint64(dst[do:], v.GetElem(0))
			binary.LittleEndian.PutUint64(dst[do+ds:], v.GetElem(1))
			po += 2 * ps
			do += 2 * ds
		}
		if y < h {
			storeBytes8(dst[do:], load8(pred[po:]).SubSaturated(dv))
		}
		return
	}
	for y := range h {
		po, do := y*ps, y*ds
		x := 0
		for ; x+16 <= w; x += 16 {
			archsimd.LoadUint8x16Array((*[16]byte)(pred[po+x : po+x+16])).SubSaturated(dv).StoreArray((*[16]byte)(dst[do+x : do+x+16]))
		}
		for ; x+8 <= w; x += 8 {
			storeBytes8(dst[do+x:], load8(pred[po+x:]).SubSaturated(dv))
		}
		for ; x < w; x += 4 {
			StorePixels4(dst[do+x:], LoadPixels4(pred[po+x:]).Add(archsimd.BroadcastInt32x4(int32(delta))))
		}
	}
}
func TMPredict(dst []byte, ds int, above, left []byte, top byte, w int) {
	if !Enabled {
		TMPredictScalar(dst, ds, above, left, top, w)
		return
	}

	if TMNative {
		if w == 4 {
			TMPredict4Native(dst, ds, above, left, top, false)
			return
		}
		TMPredictNative(dst, ds, above, left, top, w)
		return
	}

	if (w == 8 || w == 16) && tmpredictBytesX64(dst, ds, above, left, top, w) {
		return
	}

	if (w == 8 || w == 16) && TMPredictBytes(dst, ds, above, left, top, w) {
		return
	}

	t := archsimd.BroadcastInt32x4(int32(top))
	for y := range w {
		v := archsimd.BroadcastInt32x4(int32(left[y])).Sub(t)
		for x := 0; x < w; x += 4 {
			StorePixels4(dst[y*ds+x:], LoadPixels4(above[x:]).Add(v))
		}
	}
}

func loadN(b []byte, n int) archsimd.Uint8x16 {
	if n == 4 {
		return archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(b)).ReshapeToUint8s()
	}
	return load8(b)
}

// slicesOverlap reports whether the first na elements of a and first nb
// elements of b overlap, using only O(1) safe pointer equality.
// Two slices sharing a backing array share the same tail element &s[:cap(s)][cap(s)-1]
// unless cap-clipped via 3-index slicing; callers do not pass cap-clipped
// aliases of the same backing buffer.
func slicesOverlap[T any](a, b []T, na, nb int) bool {
	if na <= 0 || nb <= 0 {
		return false
	}
	if &a[0] == &b[0] {
		return true
	}
	ca, cb := cap(a), cap(b)
	if &a[:ca][ca-1] == &b[:cb][cb-1] {
		if ca >= cb {
			return ca-cb < na
		}
		return cb-ca < nb
	}
	return false
}

// slicesOverlapExact extends slicesOverlap to handle small 3-index cap-clipped
// backing aliases (e.g. unit tests constructing buf[0:n:n] and buf[k:k+n:k+n]).
// On real codec planes (where at least one slice exceeds 256 elements), this
// still runs in O(1).
func slicesOverlapExact[T any](a, b []T, na, nb int) bool {
	if slicesOverlap(a, b, na, nb) {
		return true
	}
	if na <= 0 || nb <= 0 || cap(a) > 25000 || cap(b) > 25000 || (len(a) != cap(a) && len(b) != cap(b)) {
		return false
	}
	a0, b0 := &a[0], &b[0]
	for i := 1; i < na; i++ {
		if &a[i] == b0 {
			return true
		}
	}
	for j := 1; j < nb; j++ {
		if &b[j] == a0 {
			return true
		}
	}
	return false
}

// SubpelStats fuses two bilinear passes with variance accumulation. Horizontal
// rows stay in vector registers, and each is reused by the next output row.
func SubpelStats(src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (int, uint32) {
	if !Enabled || (w != 4 && w%8 != 0) || w <= 0 || h <= 0 || ss < 0 || rs < 0 {
		return SubpelStatsScalar(src, ss, ref, rs, w, h, fx, fy)
	}

	// Exact identity filters need only the already optimized SIMD difference
	// statistics. Retain the bilinear read-window checks, including the extra
	// row/column, before taking this shortcut. Codec-sized geometry and bounded
	// nonnegative strides prove the endpoint arithmetic cannot overflow.
	fxIdentity, fyIdentity := fx == [2]int16{128, 0}, fy == [2]int16{128, 0}
	if fxIdentity && fyIdentity && w <= 64 && h <= 64 && (uint(ss)|uint(rs))>>30 == 0 {
		_ = src[h*ss+w]
		_ = ref[(h-1)*rs+w-1]
		return Stats(src, ss, ref, rs, w, h)
	}

	// Check phases first so ordinary two-axis work skips one-axis setup.
	// Original endpoint checks prove each exact row load in the bounded kernels.
	if (fxIdentity || fyIdentity) && w <= 64 && h <= 64 && (uint(ss)|uint(rs))>>30 == 0 {
		if w%16 == 0 {
			_ = src[h*ss+w]
			_ = ref[(h-1)*rs+w-1]
			if fyIdentity {
				return subpelStatsAxisHorizontal16(src, ss, ref, rs, w, h, fx)
			}
			return subpelStatsAxisVertical16(src, ss, ref, rs, w, h, fy)
		}
		if w == 8 {
			_ = src[h*ss+8]
			_ = ref[(h-1)*rs+7]
			if fyIdentity {
				return subpelStatsAxisHorizontal8(src, ss, ref, rs, h, fx)
			}
			return subpelStatsAxisVertical8(src, ss, ref, rs, h, fy)
		}
	}

	if w == 4 {
		return subpelStatsKernel[[4]byte](src, ss, ref, rs, w, h, fx, fy)
	}
	if w%16 == 0 {
		return subpelStats16(src, ss, ref, rs, w, h, fx, fy)
	}
	return subpelStatsKernel[[8]byte](src, ss, ref, rs, w, h, fx, fy)
}

// Fixed array shapes specialize exact four/eight-byte loads at compile time,
// following goav1's sample-width specialization. No lane-size branch remains
// in the selected row loop.
func subpelStatsKernel[Packet interface{ [4]byte | [8]byte }](src []byte, ss int, ref []byte, rs, w, h int, fx, fy [2]int16) (int, uint32) {
	var packet Packet
	n := len(packet)

	// Each read is exactly n bytes. These endpoints prove all source and
	// reference accesses, including the extra bilinear row and column, stay
	// in the original allocations. Neither input is written or retained.
	_ = src[h*ss+w]
	_ = ref[(h-1)*rs+w-1]
	x0, x1, y0, y1 := archsimd.BroadcastInt16x8(fx[0]), archsimd.BroadcastInt16x8(fx[1]), archsimd.BroadcastInt16x8(fy[0]), archsimd.BroadcastInt16x8(fy[1])
	round := archsimd.BroadcastInt16x8(64)
	var sum, sse archsimd.Int32x4
	for x := 0; x < w; x += n {
		a := filterShift7(mulAdd16(widen8(loadN(src[x:], n)).Mul(x0).Add(round), widen8(loadN(src[x+1:], n)), x1))
		srcOff, refOff := ss+x, x
		for range h {
			p := src[srcOff:]
			b := filterShift7(mulAdd16(widen8(loadN(p, n)).Mul(x0).Add(round), widen8(loadN(p[1:], n)), x1))
			v := filterShift7(mulAdd16(a.Mul(y0).Add(round), b, y1))
			a = b
			d := v.Sub(widen8(loadN(ref[refOff:], n)))
			sum = sum.Add(sumDiff16(d))
			sse = sse.Add(square8(d))
			srcOff += ss
			refOff += rs
		}
	}
	return int(sum32(sum)), uint32(sum32(sse))
}
func BilinearFirst(src []byte, ss int, dst []uint16, w, h int, f [2]int16) {
	if !Enabled {
		BilinearFirstScalar(src, ss, dst, w, h, f)
		return
	}

	c0, c1, r := archsimd.BroadcastInt32x4(int32(f[0])), archsimd.BroadcastInt32x4(int32(f[1])), archsimd.BroadcastInt32x4(64)
	for y := range h {
		for x := 0; x < w; x += 4 {
			v := LoadPixels4(src[y*ss+x:]).Mul(c0).Add(LoadPixels4(src[y*ss+x+1:]).Mul(c1)).Add(r).ShiftAllRight(7)
			storeU16(dst[y*w+x:], v)
		}
	}
}
func BilinearSecond(src []uint16, dst []byte, w, h int, f [2]int16) {
	if !Enabled {
		BilinearSecondScalar(src, dst, w, h, f)
		return
	}

	c0, c1, r := archsimd.BroadcastInt32x4(int32(f[0])), archsimd.BroadcastInt32x4(int32(f[1])), archsimd.BroadcastInt32x4(64)
	for y := range h {
		for x := 0; x < w; x += 4 {
			i := y*w + x
			v := loadU16(src[i:]).Mul(c0).Add(loadU16(src[i+w:]).Mul(c1)).Add(r).ShiftAllRight(7)
			StorePixels4(dst[i:], v)
		}
	}
}

func Sum(src []byte, ss, w, h int) int {
	if !Enabled {
		return SumScalar(src, ss, w, h)
	}

	var a archsimd.Int32x4
	tail := 0
	for y := range h {
		x := 0
		for ; x+8 <= w; x += 8 {
			v := widen8(load8(src[y*ss+x:]))
			a = a.Add(v.ExtendLo4ToInt32()).Add(high4(v))
		}
		for ; x < w; x++ {
			tail += int(src[y*ss+x])
		}
	}
	return int(sum32(a)) + tail
}
func Energy(src []int16) uint64 {
	if !Enabled {
		return EnergyScalar(src)
	}

	sum := uint64(0)
	i := 0
	for ; i+8 <= len(src); i += 8 {
		v := archsimd.LoadInt16x8(src[i:])
		sum += sumSquares(v.ExtendLo4ToInt32()) + sumSquares(high4(v))
	}
	for ; i < len(src); i++ {
		v := int64(src[i])
		sum += uint64(v * v)
	}
	return sum
}
func AbsSum(src []int16, n int) int {
	if !Enabled {
		return AbsSumScalar(src, n)
	}

	var a archsimd.Int32x4
	i := 0
	for ; i+8 <= n; i += 8 {
		v := archsimd.LoadInt16x8(src[i:])
		a = a.Add(v.ExtendLo4ToInt32().Abs()).Add(high4(v).Abs())
	}
	sum := int(sum32(a))
	for ; i < n; i++ {
		v := int(src[i])
		sum += max(v, -v)
	}
	return sum
}
func ProjectionRow(src []byte, ss, h int, out []int16, shift uint64) {
	if !Enabled {
		ProjectionRowScalar(src, ss, h, out, shift)
		return
	}

	var a, b archsimd.Int16x8
	for y := range h {
		v := archsimd.LoadUint8x16(src[y*ss:])
		a = a.Add(widen8(v))
		b = b.Add(widen8(highBytes(v)))
	}
	a.ShiftAllRight(shift).Store(out)
	b.ShiftAllRight(shift).Store(out[8:])
}
func VectorVariance(ref, src []int16, n, shift int) int {
	if !Enabled {
		return VectorVarianceScalar(ref, src, n, shift)
	}

	if value, ok := vectorVarianceWideR2(ref, src, n, shift); ok {
		return value
	}
	var sum, sse archsimd.Int32x4
	for i := 0; i < n; i += 4 {
		d := Load4(ref[i:]).Sub(Load4(src[i:]))
		sum = sum.Add(d)
		sse = sse.Add(d.Mul(d))
	}
	m := int(sum32(sum))
	return int(sum32(sse)) - (m * m >> uint(shift))
}

// DenoiseFirst keeps per-column signed adjustment sums (upper-clamped to
// 127 for luma), matching libvpx's first denoising pass before its decision.
func DenoiseFirst(mc []byte, ms int, avg []byte, as int, sig []byte, ss, w, inc, level int) int {
	if !Enabled {
		return DenoiseFirstScalar(mc, ms, avg, as, sig, ss, w, inc, level)
	}

	if byteDenoise16 && w == 16 && inc >= 0 && inc <= 1 && level >= 3 && level <= 5 {
		if sum, ok := denoiseFirst16Bytes(mc, ms, avg, as, sig, ss, inc, level); ok {
			return sum
		}
	}

	var columns [2]archsimd.Int16x8
	l1, l2, l3 := archsimd.BroadcastInt16x8(int16(level)), archsimd.BroadcastInt16x8(int16(level+1)), archsimd.BroadcastInt16x8(int16(level+3))
	zero := archsimd.Int16x8{}
	threshold := archsimd.BroadcastInt16x8(int16(3 + inc))
	for y := range w {
		for x := 0; x < w; x += 8 {
			m, s := widen8(load8(mc[y*ms+x:])), widen8(load8(sig[y*ss+x:]))
			d := m.Sub(s)
			a := d.Abs()
			adj := l2.IfElse(a.LessEqual(archsimd.BroadcastInt16x8(15)), l3)
			adj = l1.IfElse(a.LessEqual(archsimd.BroadcastInt16x8(7)), adj)
			adj = a.IfElse(a.LessEqual(threshold), adj)
			adj = zero.Sub(adj).IfElse(d.Less(zero), adj)
			columns[x/8] = columns[x/8].Add(adj)
			storeBytes8(avg[y*as+x:], packBytes(s.Add(adj)))
		}
	}
	total := 0
	for i := range w / 8 {
		v := columns[i]
		if w == 16 {
			v = v.Min(archsimd.BroadcastInt16x8(127))
		}
		total += int(sum32(v.ExtendLo4ToInt32().Add(high4(v))))
	}
	return total
}

func Intra4(dst []byte, ds int, border *[16]byte, indices *[4][16]byte) {
	if !Enabled {
		Intra4Scalar(dst, ds, border, indices)
		return
	}

	v := archsimd.LoadUint8x16Array(border)
	var lo, hi archsimd.Int16x8
	for i := range 4 {
		p := permute16(v, archsimd.LoadUint8x16Array(&indices[i]))
		lo = lo.Add(widen8(p))
		hi = hi.Add(widen8(highBytes(p)))
	}
	round := archsimd.BroadcastInt16x8(2)
	a := packBytes(lo.Add(round).ShiftAllRight(2)).ReshapeToUint64s().GetElem(0)
	b := packBytes(hi.Add(round).ShiftAllRight(2)).ReshapeToUint64s().GetElem(0)
	binary.LittleEndian.PutUint32(dst, uint32(a))
	binary.LittleEndian.PutUint32(dst[ds:], uint32(a>>32))
	binary.LittleEndian.PutUint32(dst[2*ds:], uint32(b))
	binary.LittleEndian.PutUint32(dst[3*ds:], uint32(b>>32))
}
func FillBlock(dst []byte, ds, w, h int, value byte) {
	if !Enabled {
		FillBlockScalar(dst, ds, w, h, value)
		return
	}

	// Prove the full fixed-width window before stores. Declining retains
	// the original row-by-row bounds and partial-write panic behavior.
	if fillFixedRows && h > 1 && h <= 64 && uint(ds)>>30 == 0 && (w == 4 || w == 8 || w == 16) && (h-1)*ds+w <= len(dst) {
		v := archsimd.BroadcastUint8x16(value)
		if h&3 == 0 {
			d2, d3 := 2*ds, 3*ds
			switch w {
			case 16:
				for row := 0; row < h; row += 4 {
					at := dst[row*ds:]
					v.Store(at)
					v.Store(at[ds:])
					v.Store(at[d2:])
					v.Store(at[d3:])
				}
			case 8:
				word := v.ReshapeToUint64s().GetElem(0)
				for row := 0; row < h; row += 4 {
					at := dst[row*ds:]
					binary.LittleEndian.PutUint64(at, word)
					binary.LittleEndian.PutUint64(at[ds:], word)
					binary.LittleEndian.PutUint64(at[d2:], word)
					binary.LittleEndian.PutUint64(at[d3:], word)
				}
			case 4:
				word := v.ReshapeToUint32s().GetElem(0)
				for row := 0; row < h; row += 4 {
					at := dst[row*ds:]
					binary.LittleEndian.PutUint32(at, word)
					binary.LittleEndian.PutUint32(at[ds:], word)
					binary.LittleEndian.PutUint32(at[d2:], word)
					binary.LittleEndian.PutUint32(at[d3:], word)
				}
			}
			return
		}
		switch w {
		case 16:
			for row := range h {
				v.Store(dst[row*ds:])
			}
		case 8:
			word := v.ReshapeToUint64s().GetElem(0)
			for row := range h {
				binary.LittleEndian.PutUint64(dst[row*ds:], word)
			}
		case 4:
			word := v.ReshapeToUint32s().GetElem(0)
			for row := range h {
				binary.LittleEndian.PutUint32(dst[row*ds:], word)
			}
		}
		return
	}
	v := archsimd.BroadcastUint8x16(value)
	for y := range h {
		v.StorePart(dst[y*ds : y*ds+w])
	}
}
func RepeatRow(dst []byte, ds int, above []byte, w int) {
	if !Enabled {
		RepeatRowScalar(dst, ds, above, w)
		return
	}
	v := loadN(above, min(w, 8))
	if w == 16 {
		v = archsimd.LoadUint8x16(above)
	}
	for y := range w {
		v.StorePart(dst[y*ds : y*ds+w])
	}
}
func Horizontal(dst []byte, ds int, left []byte, w int) {
	if !Enabled {
		HorizontalScalar(dst, ds, left, w)
		return
	}
	for y := range w {
		v := archsimd.BroadcastUint8x16(left[y])
		v.StorePart(dst[y*ds : y*ds+w])
	}
}

func Mul16(a, b, out []int16, n int) {
	if !Enabled {
		Mul16Scalar(a, b, out, n)
		return
	}
	i := 0
	for ; i+8 <= n; i += 8 {
		archsimd.LoadInt16x8(a[i:]).Mul(archsimd.LoadInt16x8(b[i:])).Store(out[i:])
	}
	for ; i < n; i++ {
		out[i] = a[i] * b[i]
	}
}

// Batch exceptionally tall strips to keep the signed vector sum exact in
// int32. Width is at most 64, so a batch sums at most 64*4096*255. SSE
// intentionally wraps modulo 2^32, matching StatsScalar.
func statsTallSIMD(src []byte, ss int, ref []byte, rs, w, h int) (int, uint32) {
	var totalSum int
	var totalSSE uint32
	for h > 4096 {
		sse, sum := pixelStatsSIMD(src, ss, ref, rs, w, 4096)
		totalSum += int(sum)
		totalSSE += sse
		src, ref = src[4096*ss:], ref[4096*rs:]
		h -= 4096
	}
	sse, sum := pixelStatsSIMD(src, ss, ref, rs, w, h)
	return totalSum + int(sum), totalSSE + sse
}

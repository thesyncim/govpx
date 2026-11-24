//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// SAD computes the sum of absolute differences for a w x h block.
// Separate low/high uint16 accumulators avoid widening to uint32 inside the
// hot loop: at most 64*64/16 = 256 byte vectors contribute per lane, so the
// maximum lane sum is 256*255 = 65280 <= 65535 (no uint16 wrap).
func SAD(src []byte, ss int, ref []byte, rs, w, h int) int {
	if !Enabled {
		return SADScalar(src, ss, ref, rs, w, h)
	}
	if w <= 0 || h <= 0 {
		return 0
	}
	if w > 64 || h > 64 || ss < 0 || rs < 0 {
		return sadGeneric(src, ss, ref, rs, w, h)
	}
	_ = src[(h-1)*ss+w-1]
	_ = ref[(h-1)*rs+w-1]

	switch w {
	case 64:
		return sad64ARM64(src, ss, ref, rs, h)
	case 32:
		return sad32ARM64(src, ss, ref, rs, h)
	case 16:
		return sad16ARM64(src, ss, ref, rs, h)
	case 8:
		if h == 8 {
			return sad8x8Slice(src, ss, ref, rs)
		}
		if h == 16 {
			return sad8x8Slice(src, ss, ref, rs) + sad8x8Slice(src[8*ss:], ss, ref[8*rs:], rs)
		}
	}

	var lo, hi archsimd.Uint16x8
	tail := 0
	for y := range h {
		a, b := src[y*ss:y*ss+w], ref[y*rs:y*rs+w]
		x := 0
		for ; x+16 <= w; x += 16 {
			va := archsimd.LoadUint8x16(a[x : x+16])
			vb := archsimd.LoadUint8x16(b[x : x+16])
			d := va.Max(vb).Sub(va.Min(vb))
			lo = lo.Add(d.ExtendLo8ToUint16())
			hi = hi.Add(d.HiToLo().ExtendLo8ToUint16())
		}
		if x+8 <= w {
			va := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(a[x:x+8])).ReshapeToUint8s()
			vb := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(b[x:x+8])).ReshapeToUint8s()
			d := va.Max(vb).Sub(va.Min(vb))
			lo = lo.Add(d.ExtendLo8ToUint16())
			x += 8
		}
		if x+4 <= w {
			va := archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(a[x:x+4])).ReshapeToUint8s()
			vb := archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(b[x:x+4])).ReshapeToUint8s()
			d := va.Max(vb).Sub(va.Min(vb))
			lo = lo.Add(d.ExtendLo8ToUint16())
			x += 4
		}
		for ; x < w; x++ {
			d := int(a[x]) - int(b[x])
			tail += max(d, -d)
		}
	}
	return reduceSADLoHi(lo, hi) + tail
}

func sad64ARM64(src []byte, ss int, ref []byte, rs, h int) int {
	var lo0, hi0, lo1, hi1 archsimd.Uint16x8
	so, ro := 0, 0
	for range h {
		a := src[so : so+64]
		b := ref[ro : ro+64]
		d0 := archsimd.LoadUint8x16(a[0:16])
		r0 := archsimd.LoadUint8x16(b[0:16])
		d0 = d0.Max(r0).Sub(d0.Min(r0))
		lo0 = lo0.Add(d0.ExtendLo8ToUint16())
		hi0 = hi0.Add(d0.HiToLo().ExtendLo8ToUint16())

		d1 := archsimd.LoadUint8x16(a[16:32])
		r1 := archsimd.LoadUint8x16(b[16:32])
		d1 = d1.Max(r1).Sub(d1.Min(r1))
		lo1 = lo1.Add(d1.ExtendLo8ToUint16())
		hi1 = hi1.Add(d1.HiToLo().ExtendLo8ToUint16())

		d2 := archsimd.LoadUint8x16(a[32:48])
		r2 := archsimd.LoadUint8x16(b[32:48])
		d2 = d2.Max(r2).Sub(d2.Min(r2))
		lo0 = lo0.Add(d2.ExtendLo8ToUint16())
		hi0 = hi0.Add(d2.HiToLo().ExtendLo8ToUint16())

		d3 := archsimd.LoadUint8x16(a[48:64])
		r3 := archsimd.LoadUint8x16(b[48:64])
		d3 = d3.Max(r3).Sub(d3.Min(r3))
		lo1 = lo1.Add(d3.ExtendLo8ToUint16())
		hi1 = hi1.Add(d3.HiToLo().ExtendLo8ToUint16())

		so += ss
		ro += rs
	}
	return reduceSADLoHi(lo0, hi0) + reduceSADLoHi(lo1, hi1)
}

func sad32ARM64(src []byte, ss int, ref []byte, rs, h int) int {
	var lo0, hi0, lo1, hi1 archsimd.Uint16x8
	so, ro := 0, 0
	for range h {
		a := src[so : so+32]
		b := ref[ro : ro+32]
		d0 := archsimd.LoadUint8x16(a[0:16])
		r0 := archsimd.LoadUint8x16(b[0:16])
		d0 = d0.Max(r0).Sub(d0.Min(r0))
		lo0 = lo0.Add(d0.ExtendLo8ToUint16())
		hi0 = hi0.Add(d0.HiToLo().ExtendLo8ToUint16())

		d1 := archsimd.LoadUint8x16(a[16:32])
		r1 := archsimd.LoadUint8x16(b[16:32])
		d1 = d1.Max(r1).Sub(d1.Min(r1))
		lo1 = lo1.Add(d1.ExtendLo8ToUint16())
		hi1 = hi1.Add(d1.HiToLo().ExtendLo8ToUint16())

		so += ss
		ro += rs
	}
	return reduceSADLoHi(lo0, hi0) + reduceSADLoHi(lo1, hi1)
}

func sad16ARM64(src []byte, ss int, ref []byte, rs, h int) int {
	var lo0, hi0, lo1, hi1 archsimd.Uint16x8
	so, ro := 0, 0
	y := 0
	for ; y+1 < h; y += 2 {
		a0 := archsimd.LoadUint8x16(src[so : so+16])
		b0 := archsimd.LoadUint8x16(ref[ro : ro+16])
		d0 := a0.Max(b0).Sub(a0.Min(b0))
		lo0 = lo0.Add(d0.ExtendLo8ToUint16())
		hi0 = hi0.Add(d0.HiToLo().ExtendLo8ToUint16())

		so += ss
		ro += rs

		a1 := archsimd.LoadUint8x16(src[so : so+16])
		b1 := archsimd.LoadUint8x16(ref[ro : ro+16])
		d1 := a1.Max(b1).Sub(a1.Min(b1))
		lo1 = lo1.Add(d1.ExtendLo8ToUint16())
		hi1 = hi1.Add(d1.HiToLo().ExtendLo8ToUint16())

		so += ss
		ro += rs
	}
	if y < h {
		a0 := archsimd.LoadUint8x16(src[so : so+16])
		b0 := archsimd.LoadUint8x16(ref[ro : ro+16])
		d0 := a0.Max(b0).Sub(a0.Min(b0))
		lo0 = lo0.Add(d0.ExtendLo8ToUint16())
		hi0 = hi0.Add(d0.HiToLo().ExtendLo8ToUint16())
	}
	return reduceSADLoHi(lo0, hi0) + reduceSADLoHi(lo1, hi1)
}

func reduceSADLoHi(lo, hi archsimd.Uint16x8) int {
	sum := lo.ExtendLo4ToUint32().Add(lo.HiToLo().ExtendLo4ToUint32()).
		Add(hi.ExtendLo4ToUint32()).Add(hi.HiToLo().ExtendLo4ToUint32())
	return int(sum.ReduceSum())
}

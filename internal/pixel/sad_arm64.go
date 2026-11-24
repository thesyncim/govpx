//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// SAD keeps separate low/high uint16 accumulators until the final widening
// reduction. At most 64*64/16 byte vectors contribute to each lane, so the
// maximum is 256*255=65280 and cannot wrap. The endpoint checks prove every
// unaligned fixed-width load below lies inside its source slice, including
// the final row.
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
	var lo, hi archsimd.Uint16x8
	tail := 0
	if w == 8 && (h == 8 || h == 16) {
		value := sad8x8Slice(src, ss, ref, rs)
		if h == 16 {
			value += sad8x8Slice(src[8*ss:], ss, ref[8*rs:], rs)
		}
		return value
	}
	for y := range h {
		a, b := src[y*ss:], ref[y*rs:]
		x := 0
		for ; x+16 <= w; x += 16 {
			va := archsimd.LoadUint8x16(a[x:])
			vb := archsimd.LoadUint8x16(b[x:])
			d := va.Max(vb).Sub(va.Min(vb))
			lo = lo.Add(d.ExtendLo8ToUint16())
			hi = hi.Add(d.HiToLo().ExtendLo8ToUint16())
		}
		if x+8 <= w {
			va := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(a[x:])).ReshapeToUint8s()
			vb := archsimd.Uint64x2{}.SetElem(0, binary.LittleEndian.Uint64(b[x:])).ReshapeToUint8s()
			d := va.Max(vb).Sub(va.Min(vb))
			lo = lo.Add(d.ExtendLo8ToUint16())
			x += 8
		}
		if x+4 <= w {
			va := archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(a[x:])).ReshapeToUint8s()
			vb := archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(b[x:])).ReshapeToUint8s()
			d := va.Max(vb).Sub(va.Min(vb))
			lo = lo.Add(d.ExtendLo8ToUint16())
			x += 4
		}
		for ; x < w; x++ {
			d := int(a[x]) - int(b[x])
			tail += max(d, -d)
		}
	}
	sum := lo.ExtendLo4ToUint32().Add(lo.HiToLo().ExtendLo4ToUint32()).Add(hi.ExtendLo4ToUint32()).Add(hi.HiToLo().ExtendLo4ToUint32())
	return int(sum.ReduceSum()) + tail
}

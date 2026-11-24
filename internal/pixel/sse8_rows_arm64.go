//go:build goexperiment.simd && arm64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// SSE8 packs pairs of exact eight-byte rows into one SIMD packet. An odd
// final row reads eight bytes, including for a minimally sized allocation.
func SSE8(src []byte, ss int, ref []byte, rs, h int) uint32 {
	if !Enabled {
		return SSE8Scalar(src, ss, ref, rs, h)
	}

	// Bounded nonnegative geometry proves the final row offsets fit int64.
	// The two endpoint checks cover exact8-byte loads, including unaligned,
	// zero-stride and overlapping read-only rows, without requiring padding.
	if h > 0 && h <= 64 && (uint(ss)|uint(rs))>>30 == 0 {
		_ = src[(h-1)*ss+7]
		_ = ref[(h-1)*rs+7]
		var lo, hi archsimd.Int32x4
		so, ro, y := 0, 0, 0
		for ; y+2 <= h; y += 2 {
			a := (archsimd.Uint64x2{}).SetElem(0, binary.LittleEndian.Uint64(src[so:])).SetElem(1, binary.LittleEndian.Uint64(src[so+ss:])).ReshapeToUint8s()
			b := (archsimd.Uint64x2{}).SetElem(0, binary.LittleEndian.Uint64(ref[ro:])).SetElem(1, binary.LittleEndian.Uint64(ref[ro+rs:])).ReshapeToUint8s()
			d := a.Max(b).Sub(a.Min(b))
			dl := widen8(d)
			dh := widen8(highBytes(d))
			lo = lo.Add(square8(dl))
			hi = hi.Add(square8(dh))
			so += 2 * ss
			ro += 2 * rs
		}
		if y < h {
			a := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(src[so:])).ReshapeToUint8s()
			b := archsimd.BroadcastUint64x2(binary.LittleEndian.Uint64(ref[ro:])).ReshapeToUint8s()
			lo = lo.Add(square8(widen8(a.Max(b).Sub(a.Min(b)))))
		}
		return uint32(sum32(lo.Add(hi)))
	}
	var lo, hi archsimd.Int32x4
	y := 0
	for ; y+2 <= h; y += 2 {
		lo, hi = ssePacket(lo, hi, packRows8(src, y*ss, ss), packRows8(ref, y*rs, rs))
	}
	if y < h {
		lo, hi = ssePacket(lo, hi, load8(src[y*ss:]), load8(ref[y*rs:]))
	}
	return uint32(sum32(lo.Add(hi)))
}

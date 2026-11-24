//go:build goexperiment.simd && amd64 && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// AddConstantPair4 adds independent clamped DC deltas to two adjacent 4x4
// blocks using all sixteen byte lanes. Partial aliases retain block order in
// the caller; exact in-place layouts and disjoint buffers can run together.
func AddConstantPair4(pred []byte, ps int, dst []byte, ds int, d0, d1 int16) bool {
	if !Enabled || ps < 8 || ds < 8 || len(pred) < 8 || len(dst) < 8 || ps > (len(pred)-8)/3 || ds > (len(dst)-8)/3 || d0 < -255 || d0 > 255 || d1 < -255 || d1 > 255 {
		return false
	}
	if &pred[0] == &dst[0] {
		if ps != ds {
			return false
		}
	} else if slicesOverlapExact(pred, dst, len(pred), len(dst)) {
		return false
	}

	// Each byte lane has either a positive or a negative delta, never both.
	// Unsigned saturation therefore clips exactly without widening pixels.
	pattern := func(lo, hi int16) archsimd.Uint8x16 {
		a, b := uint32(byte(lo))*0x01010101, uint32(byte(hi))*0x01010101
		return archsimd.BroadcastUint64x2(uint64(a) | uint64(b)<<32).ReshapeToUint8s()
	}
	plus := pattern(max(d0, 0), max(d1, 0))
	minus := pattern(max(-d0, 0), max(-d1, 0))
	rows := func(y int) {
		v := packRows8(pred, y*ps, ps).AddSaturated(plus).SubSaturated(minus).ReshapeToUint64s()
		binary.LittleEndian.PutUint64(dst[y*ds:], v.GetElem(0))
		binary.LittleEndian.PutUint64(dst[(y+1)*ds:], v.GetElem(1))
	}
	rows(0)
	rows(2)
	return true
}

//go:build goexperiment.simd && (arm64 || amd64) && !purego

package pixel

import (
	"encoding/binary"
	"simd/archsimd"
)

// LoopFilterUV8 joins two disjoint eight-position chroma edges into one
// sixteen-lane packet. Aliased planes retain sequential scalar/native calls.
func LoopFilterUV8(u, v []byte, step, outer, blimit, limit, thresh, kind int) bool {
	if !Enabled || (kind != 4 && kind != 6) || blimit < 0 || blimit >= 255 || limit < 0 || limit > 255 || thresh < 0 || thresh > 255 {
		return false
	}
	if !((outer == 1 && step >= 8) || (step == 1 && outer >= 8)) {
		return false
	}
	// An eight-by-eight tile ends at 7*pitch+7. Dividing before
	// multiplying keeps the proof valid even for oversized stride arguments.
	if min(len(u), len(v)) < 8 || max(step, outer) > (min(len(u), len(v))-8)/7 {
		return false
	}
	if slicesOverlap(u, v, len(u), len(v)) {
		return false
	}
	loopFilterUVBytes(u, v, 4*step, step, outer, blimit, limit, thresh, kind)
	return true
}
func loadLFUVByte(u, v []byte, off int) archsimd.Uint8x16 {
	return joinLFBytes(loadLFByte(u, off, 8), loadLFByte(v, off, 8))
}
func loadLFUVColumns(u, v []byte, off, pitch int) (archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16, archsimd.Uint8x16) {
	a0, a1, a2, a3, a4, a5, a6, a7 := loadLFColumnTile[[8]byte](u, off, pitch)
	b0, b1, b2, b3, b4, b5, b6, b7 := loadLFColumnTile[[8]byte](v, off, pitch)
	return joinLFBytes(a0, b0), joinLFBytes(a1, b1), joinLFBytes(a2, b2), joinLFBytes(a3, b3), joinLFBytes(a4, b4), joinLFBytes(a5, b5), joinLFBytes(a6, b6), joinLFBytes(a7, b7)
}
func storeLFUV(u, v []byte, off, step, outer, count int, a, b, c, d, e, f archsimd.Uint8x16) {
	if outer == 1 {
		store := func(at int, x archsimd.Uint8x16) {
			words := x.ReshapeToUint64s()
			binary.LittleEndian.PutUint64(u[at:], words.GetElem(0))
			binary.LittleEndian.PutUint64(v[at:], words.GetElem(1))
		}
		store(off, a)
		store(off+step, b)
		if count >= 4 {
			store(off+2*step, c)
			store(off+3*step, d)
		}
		if count == 6 {
			store(off+4*step, e)
			store(off+5*step, f)
		}
		return
	}
	storeLFByteTile(u, off, outer, count, a, b, c, d, e, f)
	storeLFByteTile(v, off, outer, count, highBytes(a), highBytes(b), highBytes(c), highBytes(d), highBytes(e), highBytes(f))
}

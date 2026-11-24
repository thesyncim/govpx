//go:build goexperiment.simd && arm64 && !purego

// Ported from libvpx v1.16.0.
package dsp

import (
	"encoding/binary"
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

func sse4PackRows(p []byte, s int) archsimd.Uint8x16 {
	return archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(p)).SetElem(1, binary.LittleEndian.Uint32(p[s:])).SetElem(2, binary.LittleEndian.Uint32(p[2*s:])).SetElem(3, binary.LittleEndian.Uint32(p[3*s:])).ReshapeToUint8s()
}
func sse4RowsGo(src []byte, ss int, ref []byte, rs, h int) (int, bool) {
	if !pixel.Enabled || ss < 0 || rs < 0 || len(src) < 4 || len(ref) < 4 || ss > (len(src)-4)/(h-1) || rs > (len(ref)-4)/(h-1) {
		return 0, false
	}
	var total archsimd.Int32x4
	for y := 0; y < h; y += 4 {
		a, b := sse4PackRows(src[y*ss:], ss), sse4PackRows(ref[y*rs:], rs)
		d := a.Max(b).Sub(a.Min(b))
		lo, hi := d.ExtendLo8ToUint16().BitsToInt16(), d.HiToLo().ExtendLo8ToUint16().BitsToInt16()
		total = total.Add(lo.MulWidenLo(lo)).Add(lo.HiToLo().MulWidenLo(lo.HiToLo())).Add(hi.MulWidenLo(hi)).Add(hi.HiToLo().MulWidenLo(hi.HiToLo()))
	}
	// Exactly 32 squares of at most 255 fit signed int32.
	return int(total.ReduceSum()), true
}

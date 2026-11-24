//go:build goexperiment.simd && arm64 && !purego

// Ported from libvpx v1.16.0.
package dsp

import (
	"encoding/binary"
	"github.com/thesyncim/govpx/internal/pixel"
	"simd/archsimd"
)

func sad4RowsGo(src []byte, ss int, ref []byte, rs int) (int, bool) {
	if !pixel.Enabled || ss < 0 || rs < 0 || len(src) < 4 || len(ref) < 4 || ss > (len(src)-4)/3 || rs > (len(ref)-4)/3 {
		return 0, false
	}
	a := archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(src)).SetElem(1, binary.LittleEndian.Uint32(src[ss:])).SetElem(2, binary.LittleEndian.Uint32(src[2*ss:])).SetElem(3, binary.LittleEndian.Uint32(src[3*ss:])).ReshapeToUint8s()
	b := archsimd.Uint32x4{}.SetElem(0, binary.LittleEndian.Uint32(ref)).SetElem(1, binary.LittleEndian.Uint32(ref[rs:])).SetElem(2, binary.LittleEndian.Uint32(ref[2*rs:])).SetElem(3, binary.LittleEndian.Uint32(ref[3*rs:])).ReshapeToUint8s()
	d := a.Max(b).Sub(a.Min(b))
	hi := d.ReshapeToUint64s().InterleaveHi(d.ReshapeToUint64s()).ReshapeToUint8s()
	return int(d.ExtendLo8ToUint16().Add(hi.ExtendLo8ToUint16()).ReduceSum()), true
}
